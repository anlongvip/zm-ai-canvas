package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/provider/runninghub"
)

func TestRunningHubQueuedCancellationRefundsWithoutUpstream(t *testing.T) {
	f := newRunningHubEntryFixture(t, "plus")
	task, err := f.s.CreateTask("user", f.canvasRequest())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		cancelled, err := f.s.CancelTask(t.Context(), "user", task.ID)
		if err != nil || cancelled.Status != model.TaskStatusCancelled {
			t.Fatalf("queued cancellation: task=%#v err=%v", cancelled, err)
		}
	}
	order, err := f.s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	account, err := f.s.repo.CreditAccount("user")
	if err != nil {
		t.Fatal(err)
	}
	var refunds int64
	if err := f.db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerRefund).Count(&refunds).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusRefunded || account.AvailableMicrocredits != 10_000_000 || account.ReservedMicrocredits != 0 || refunds != 1 || len(f.paths) != 0 {
		t.Fatalf("queued cancellation must refund once without upstream calls: order=%#v account=%#v refunds=%d paths=%v", order, account, refunds, f.paths)
	}
}

func TestRunningHubInternalCancellationReconcilesBilling(t *testing.T) {
	for _, status := range []string{"CANCELLED", "CANCELED", "FAILED", "SUCCESS", "RUNNING", "missing"} {
		t.Run(status, func(t *testing.T) {
			f := newRunningHubEntryFixture(t, "plus")
			cancels, queries := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer entry-test-key" {
					t.Error("missing server credential")
				}
				switch r.URL.Path {
				case "/task/openapi/cancel":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["taskId"] != "original-task" {
						t.Errorf("cancel must use original task: %#v %v", body, err)
					}
					cancels++
					_, _ = io.WriteString(w, `{"code":0,"data":null}`)
				case runninghub.QueryPath:
					var body runninghub.QueryRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TaskID != "original-task" {
						t.Errorf("query must use original task: %#v %v", body, err)
					}
					queries++
					if status == "missing" {
						_, _ = io.WriteString(w, `{"taskId":"original-task","status":"","errorCode":"1004"}`)
					} else {
						_, _ = fmt.Fprintf(w, `{"taskId":"original-task","status":%q,"results":[{"url":"https://example.com/result.mp4","outputType":"mp4","nodeId":"364"}],"usage":{"taskCostTime":81.1,"consumeCoins":3,"consumeMoney":"0.135"}}`, status)
					}
				default:
					t.Errorf("cancellation must not submit a new task: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			if err := f.db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Update("base_url", server.URL).Error; err != nil {
				t.Fatal(err)
			}
			task, err := f.s.CreateTask("user", f.canvasRequest())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusRunning, "stage": "上游生成中", "provider_request_id": "original-task"}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.CancelTask(t.Context(), "user", task.ID); err == nil {
				t.Fatal("user must not cancel a submitted task")
			}
			// Exercise the synchronous drain path so the initial request has finished
			// before checking funds; the same coordinator serves background workers.
			f.s.BeginDrain()
			cancelled, err := f.s.taskLifecycle().cancelTaskWithIntent(t.Context(), "user", task.ID, model.TaskCancellationIntent{Source: model.TaskCancellationParentFailed})
			if err != nil || cancelled.Status != model.TaskStatusCancelled {
				t.Fatalf("internal cancellation: %#v %v", cancelled, err)
			}
			order, err := f.s.repo.BillingOrder(task.BillingOrderID)
			if err != nil || order.Status != model.BillingStatusReserved || order.ReservedAmountMicrocredits != f.sale*5 {
				t.Fatalf("cancel acceptance must not refund: %#v %v", order, err)
			}
			stored, err := f.s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.s.reconcileProviderCancellation(t.Context(), stored); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.CancelTask(t.Context(), "user", task.ID); err != nil {
				t.Fatal(err)
			}
			order, err = f.s.repo.BillingOrder(task.BillingOrderID)
			if err != nil {
				t.Fatal(err)
			}
			account, err := f.s.repo.CreditAccount("user")
			if err != nil {
				t.Fatal(err)
			}
			stored, err = f.s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantAvailable, wantReserved := model.BillingStatusRefunded, int64(10_000_000), int64(0)
			wantCancelStatus := model.ProviderCancelStatusUncertain
			switch status {
			case "CANCELLED", "CANCELED":
				wantCancelStatus = model.ProviderCancelStatusConfirmed
				if stored.ProviderCancelledAt == nil {
					t.Fatal("confirmed cancellation must preserve confirmation time")
				}
			case "SUCCESS":
				wantStatus, wantAvailable = model.BillingStatusSettled, 10_000_000-f.sale*82
				cost, err := billingCreditCost(*order)
				if err != nil || cost == nil || *cost != f.cost*82 || order.Quantity != 82 || order.CostQuantity != 82 {
					t.Fatalf("completed task must settle actual runtime and cost: order=%#v cost=%v err=%v", order, cost, err)
				}
			case "RUNNING":
				wantCancelStatus = model.ProviderCancelStatusRequested
				wantStatus, wantAvailable, wantReserved = model.BillingStatusReserved, 10_000_000-f.sale*5, f.sale*5
				if order.Status == model.BillingStatusSettled || order.Status == model.BillingStatusRefunded || stored.ProviderCancelStatus != model.ProviderCancelStatusRequested || stored.ProviderCancelNextCheckAt == nil {
					t.Fatalf("running task must remain reserved and scheduled: %#v %#v", stored, order)
				}
			case "missing":
				wantStatus, wantAvailable, wantReserved = model.BillingStatusUncertain, 10_000_000-f.sale*5, f.sale*5
				if stored.ProviderCancelStatus != model.ProviderCancelStatusUncertain || stored.ProviderCancelNextCheckAt != nil {
					t.Fatalf("missing task must stop automatic reconciliation: %#v", stored)
				}
			}
			var refunds int64
			if err := f.db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerRefund).Count(&refunds).Error; err != nil {
				t.Fatal(err)
			}
			wantRefunds := int64(0)
			if wantStatus == model.BillingStatusRefunded {
				wantRefunds = 1
			}
			if cancels != 1 || queries != 1 || stored.Status != model.TaskStatusCancelled || stored.ProviderCancelStatus != wantCancelStatus || order.Status != wantStatus || account.AvailableMicrocredits != wantAvailable || account.ReservedMicrocredits != wantReserved || refunds != wantRefunds {
				t.Fatalf("incorrect cancellation settlement: cancels=%d queries=%d task=%#v order=%#v account=%#v refunds=%d", cancels, queries, stored, order, account, refunds)
			}
		})
	}
}
