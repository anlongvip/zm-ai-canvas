package app

import (
	"errors"
	"reflect"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestRunningHubRefundedRuntimeInsufficientBalanceRemainsAuditable(t *testing.T) {
	svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	if err := svc.taskBilling().RefundBilling(task.BillingOrderID, "failed"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", task.UserID).
		Updates(map[string]any{"available_microcredits": 100, "reserved_microcredits": 500000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, repository.ErrInsufficientCredits) {
		t.Fatalf("expected insufficient balance, got %v", err)
	}
	for range 2 {
		if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, errTaskBillingReview) {
			t.Fatalf("refunded recovery should stay in review, got %v", err)
		}
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.repo.CreditAccount(task.UserID)
	if err != nil {
		t.Fatal(err)
	}
	var consumed int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).Count(&consumed).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusUncertain || order.RefundedAt == nil || order.Error == "" || order.Quantity != 121 || order.CostQuantity != 121 ||
		order.ActualAmountMicrocredits != 0 || order.ProviderRequestID != task.ProviderRequestID || consumed != 0 || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 500000 {
		t.Fatalf("failed runtime recharge lost review or consumed another reservation: order=%#v account=%#v consumed=%d", order, account, consumed)
	}
}

func TestRunningHubRefundedRuntimeAdminReviewResolution(t *testing.T) {
	for _, action := range []string{"settle", "refund"} {
		t.Run(action, func(t *testing.T) {
			svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
			if err := db.AutoMigrate(&model.AdminAuditEvent{}); err != nil {
				t.Fatal(err)
			}
			if err := svc.RefundBilling(task.BillingOrderID, "outcome unknown"); err != nil {
				t.Fatal(err)
			}
			other := &model.BillingOrder{ID: "other-order", UserID: task.UserID, IdempotencyKey: "other-task", Status: model.BillingStatusReserved, Capability: "image", BillingMode: "fixed_request", AmountMicrocredits: 500000, Quantity: 1}
			if err := svc.repo.ReserveBillingOrder(other); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", task.UserID).Update("available_microcredits", 100).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, repository.ErrInsufficientCredits) {
				t.Fatal(err)
			}
			admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
			req := ResolveBillingRequest{Action: action, Note: "verified upstream runtime and retain or recharge refund"}
			if _, err := svc.ResolveBillingOrder(&model.User{Role: model.UserRoleUser}, task.BillingOrderID, req); err == nil {
				t.Fatal("non-admin could resolve runtime review")
			}
			if _, err := svc.ResolveBillingOrder(admin, task.BillingOrderID, ResolveBillingRequest{Action: action}); err == nil {
				t.Fatal("runtime review accepted no reason")
			}
			if action == "settle" {
				if _, err := svc.ResolveBillingOrder(admin, task.BillingOrderID, req); !errors.Is(err, repository.ErrInsufficientCredits) {
					t.Fatalf("admin could charge frozen credits: %v", err)
				}
				if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", task.UserID).Update("available_microcredits", 400000).Error; err != nil {
					t.Fatal(err)
				}
			}
			before, _ := svc.repo.CreditAccount(task.UserID)
			// Even after a top-up, automated recovery cannot resolve the review.
			if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, errTaskBillingReview) {
				t.Fatalf("automated recovery bypassed admin review: %v", err)
			}
			resolved, err := svc.ResolveBillingOrder(admin, task.BillingOrderID, req)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := svc.repo.CreditAccount(task.UserID)
			otherAfter, _ := svc.repo.BillingOrder(other.ID)
			var consumes, refunds, audits int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerConsume).Count(&consumes)
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerRefund).Count(&refunds)
			db.Model(&model.AdminAuditEvent{}).Where("action = ?", "billing.resolve").Count(&audits)
			if after.ReservedMicrocredits != 500000 || otherAfter.Status != model.BillingStatusReserved || resolved.ResolvedBy != admin.ID || resolved.ResolutionNote != req.Note || refunds != 1 || audits != 1 {
				t.Fatalf("review resolution lost audit or another reservation: %#v %#v other=%#v refunds=%d audits=%d", resolved, after, otherAfter, refunds, audits)
			}
			if action == "settle" {
				cost, costErr := billingCreditCost(*resolved)
				if resolved.Status != model.BillingStatusSettled || resolved.Quantity != 121 || resolved.CostQuantity != 121 || resolved.ActualAmountMicrocredits != 302500 ||
					after.AvailableMicrocredits != 97500 || consumes != 1 || costErr != nil || cost == nil || *cost != 1667*121 {
					t.Fatalf("admin runtime settlement incorrect: %#v %#v consumes=%d cost=%v err=%v", resolved, after, consumes, cost, costErr)
				}
			} else {
				if resolved.Status != model.BillingStatusRefunded || resolved.RefundedAt == nil || resolved.ActualAmountMicrocredits != 0 || consumes != 0 || !reflect.DeepEqual(before, after) {
					t.Fatalf("closing review moved credits: %#v %#v consumes=%d", resolved, after, consumes)
				}
				if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, repository.ErrBillingStateConflict) {
					t.Fatalf("automatic recovery reopened confirmed refund: %v", err)
				}
			}
			if _, err := svc.ResolveBillingOrder(admin, task.BillingOrderID, req); err == nil {
				t.Fatal("resolved review accepted another admin transition")
			}
			final, _ := svc.repo.CreditAccount(task.UserID)
			if !reflect.DeepEqual(after, final) {
				t.Fatal("repeated admin resolution changed funds")
			}
		})
	}
}
