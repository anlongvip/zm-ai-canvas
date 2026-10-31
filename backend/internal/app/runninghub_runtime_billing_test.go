package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/provider/runninghub"
	"yingce/backend/internal/repository"
)

func newRunningHubBillingFixture(t *testing.T, quantity, available, multiplier int64) (*Service, *gorm.DB, *model.Task) {
	t.Helper()
	db := newSQLiteTestDB(t)
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}, &model.CreditAccount{}, &model.CreditLedgerEntry{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	task := &model.Task{
		ID: "runtime-task", UserID: "runtime-user", Type: "canvas_video", BillingOrderID: "runtime-order",
		ProviderRequestID: "rh-task", Status: model.TaskStatusRunning,
		InputJSON:  `{"mode":"video","config":{"channelId":"rh-channel","interfaceType":"runninghub-workflow-video","videoSeconds":"5"}}`,
		ResultJSON: `{"mode":"video","video":{"durationMs":5000},"usage":{"taskCostTime":120.1,"consumeCoins":15,"consumeMoney":"0.2"}}`,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: task.UserID, AvailableMicrocredits: available}).Error; err != nil {
		t.Fatal(err)
	}
	amount, err := creditAmount(2500, quantity, multiplier)
	if err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID, IdempotencyKey: "task:runtime",
		ChannelID: "rh-channel", Capability: "video", BillingMode: "per_second", Status: model.BillingStatusRunning,
		UnitPriceMicrocredits: 2500, Quantity: quantity, MultiplierBasisPoints: multiplier, AmountMicrocredits: amount,
		ReservedAmountMicrocredits: amount,
		BillingCostSnapshot: model.BillingCostSnapshot{CostBillingMode: "per_second", CostQuantity: quantity,
			CostPricing: model.CreditCostPricing{Configured: true, UnitPriceMicrocredits: 1667}},
	}
	if err := svc.repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	return svc, db, task
}

type runningHubTerminalOutput struct {
	*Service
}

func (*runningHubTerminalOutput) RegisterTaskOutputFromTask(model.Task) error { return nil }

func runningHubTerminalForTest(svc *Service, task *model.Task) *taskTerminalCoordinator {
	return &taskTerminalCoordinator{
		repo: &taskTerminalRepositoryStub{task: task}, billing: svc.taskBilling(),
		replay: &taskTerminalReplayStub{}, logger: &taskTerminalLoggerStub{}, outputs: &runningHubTerminalOutput{Service: svc},
		userFacingMessage: func(err error) string { return err.Error() },
	}
}

func TestRunningHubTerminalSettlesRuntimeAndCostOnce(t *testing.T) {
	for _, test := range []struct {
		name       string
		runtime    any
		quantity   int64
		multiplier int64
	}{
		{name: "refund unused reservation", runtime: "2.1", quantity: 3, multiplier: 10000},
		{name: "supplement uses runtime not five second video", runtime: 120.1, quantity: 121, multiplier: 10000},
		{name: "group multiplier affects sale only", runtime: 120, quantity: 120, multiplier: 12500},
	} {
		t.Run(test.name, func(t *testing.T) {
			const initial = int64(10_000_000)
			svc, db, task := newRunningHubBillingFixture(t, 5, initial, test.multiplier)
			task.ResultJSON = mustJSON(map[string]any{"video": map[string]any{"durationMs": 5000}, "usage": map[string]any{
				"taskCostTime": test.runtime, "consumeCoins": 999999, "consumeMoney": "888.8", "thirdPartyConsumeMoney": "1.2",
			}})
			terminal := runningHubTerminalForTest(svc, task)
			for range 2 {
				if err := terminal.handleSuccess(task); err != nil {
					t.Fatal(err)
				}
			}
			order, err := svc.repo.BillingOrder(task.BillingOrderID)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := creditAmount(2500, test.quantity, test.multiplier)
			reserved, _ := creditAmount(2500, 5, test.multiplier)
			if order.Status != model.BillingStatusSettled || order.Quantity != test.quantity || order.ActualAmountMicrocredits != actual ||
				order.ReservedAmountMicrocredits != reserved || order.RefundedAmountMicrocredits != max(reserved-actual, 0) || order.ProviderRequestID != task.ProviderRequestID {
				t.Fatalf("incorrect runtime settlement: %#v", order)
			}
			cost, err := billingCreditCost(*order)
			if err != nil || cost == nil || *cost != 1667*test.quantity || order.CostQuantity != test.quantity {
				t.Fatalf("runtime cost = %v, %v; order=%#v", cost, err, order)
			}
			records, err := svc.analyticsFinancialRecords([]model.ApiCallLog{
				{ID: "create", UserID: task.UserID, ChannelID: order.ChannelID, BillingOrderID: order.ID, RequestKind: "create", Billable: true},
				{ID: "poll", UserID: task.UserID, ChannelID: order.ChannelID, BillingOrderID: order.ID, RequestKind: "poll", Billable: true},
			})
			if err != nil || len(records) != 1 {
				t.Fatalf("runtime finance records = %#v, %v", records, err)
			}
			finance := AnalyticsFinance{}
			finance.add(records["create"])
			if finance.RevenueMicrocredits != actual || finance.CostMicrocredits != *cost || finance.ProfitMicrocredits == nil || *finance.ProfitMicrocredits != actual-*cost {
				t.Fatalf("incorrect runtime revenue/cost/profit: %#v", finance)
			}
			account, err := svc.repo.CreditAccount(task.UserID)
			if err != nil || account.AvailableMicrocredits != initial-actual || account.ReservedMicrocredits != 0 {
				t.Fatalf("incorrect account: %#v, %v", account, err)
			}
			var consume int64
			if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).Count(&consume).Error; err != nil || consume != 1 {
				t.Fatalf("consume ledger count = %d, %v", consume, err)
			}
			var usage struct{ Usage map[string]any }
			if json.Unmarshal([]byte(task.ResultJSON), &usage) != nil || usage.Usage["consumeMoney"] != "888.8" || usage.Usage["consumeCoins"] != float64(999999) {
				t.Fatal("settlement changed upstream spend audit")
			}
		})
	}
}

func TestRunningHubRuntimeRejectsInvalidUsage(t *testing.T) {
	for _, raw := range []string{
		``, `{`, `null`, `{}`, `{"usage":null}`, `{"usage":{"taskCostTime":null}}`,
		`{"usage":{"taskCostTime":true}}`, `{"usage":{"taskCostTime":0}}`, `{"usage":{"taskCostTime":-1}}`,
		`{"usage":{"taskCostTime":"NaN"}}`, `{"usage":{"taskCostTime":"Inf"}}`,
		`{"usage":{"taskCostTime":"1e309"}}`, `{"usage":{"taskCostTime":1e100}}`,
		fmt.Sprintf(`{"usage":{"taskCostTime":%g}}`, float64(math.MaxInt64)/1000),
	} {
		t.Run(raw, func(t *testing.T) {
			if duration, err := runningHubRuntimeDurationMs(raw); err == nil || duration != 0 {
				t.Fatalf("invalid runtime accepted: %d, %v", duration, err)
			}
		})
	}
	for _, test := range []struct {
		raw  string
		want int64
	}{
		{`{"usage":{"taskCostTime":0.00001}}`, 1},
		{`{"usage":{"taskCostTime":" 3.125 "}}`, 3125},
		{`{"usage":{"taskCostTime":120}}`, 120000},
	} {
		if got, err := runningHubRuntimeDurationMs(test.raw); err != nil || got != test.want {
			t.Fatalf("runtime = %d, %v, want %d", got, err, test.want)
		}
	}
}

func TestRunningHubSettlementFailureKeepsReservationForReview(t *testing.T) {
	for _, reason := range []string{"missing usage", "missing result", "invalid mode", "invalid cost mode", "insufficient supplement", "charge limit", "wrong owner", "invalid input"} {
		t.Run(reason, func(t *testing.T) {
			svc, db, task := newRunningHubBillingFixture(t, 5, 100_000, 10000)
			switch reason {
			case "missing usage":
				task.ResultJSON = `{"video":{"durationMs":5000}}`
			case "missing result":
				task.ResultJSON = ""
			case "invalid mode":
				db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).Update("billing_mode", "fixed_request")
			case "invalid cost mode":
				db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).Update("cost_billing_mode", "fixed_request")
			case "charge limit":
				db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).Update("charge_limit_microcredits", 20_000)
			case "wrong owner":
				task.UserID = "someone-else"
			case "invalid input":
				task.InputJSON = "{"
			}
			if err := runningHubTerminalForTest(svc, task).handleSuccess(task); err == nil {
				t.Fatal("invalid settlement succeeded")
			}
			order, _ := svc.repo.BillingOrder(task.BillingOrderID)
			account, _ := svc.repo.CreditAccount("runtime-user")
			if order.Status != model.BillingStatusUncertain || account.ReservedMicrocredits != 12500 || account.AvailableMicrocredits != 87500 || order.ActualAmountMicrocredits != 0 {
				t.Fatalf("failed settlement changed funds: order=%#v account=%#v", order, account)
			}
		})
	}
}

func addRunningHubBillingPoll(t *testing.T, db *gorm.DB, task *model.Task, body string) {
	t.Helper()
	log := model.ApiCallLog{
		ID: newID(), UserID: task.UserID, TaskID: task.ID, BillingOrderID: task.BillingOrderID, ChannelID: "rh-channel",
		Capability: "video", RequestKind: "poll", Path: runninghub.QueryPath, Method: "POST", StatusCode: 200,
		Status: model.ApiCallStatusSucceeded, ResponseBody: SanitizeAPICallPayload([]byte(body), "application/json"), CreatedAt: time.Now(),
	}
	if err := db.Create(&log).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRunningHubRecoveredBillingUsesAuthenticatedRuntimeEvidence(t *testing.T) {
	for _, status := range []model.BillingStatus{model.BillingStatusRunning, model.BillingStatusUncertain, model.BillingStatusRefunded} {
		t.Run(string(status), func(t *testing.T) {
			svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
			if status == model.BillingStatusRefunded {
				if err := svc.taskBilling().RefundBilling(task.BillingOrderID, "failed"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).Update("status", status).Error; err != nil {
					t.Fatal(err)
				}
			}
			// Media recovery can reconstruct the video without copying usage.
			task.ResultJSON = `{"mode":"video","video":{"durationMs":5000}}`
			addRunningHubBillingPoll(t, db, task, `{"taskId":"rh-task","status":"SUCCESS","usage":{"taskCostTime":"61.1","consumeCoins":3,"consumeMoney":"0.07"}}`)
			for range 2 {
				if err := svc.settleRecoveredBilling(task, "rh-task"); err != nil {
					t.Fatal(err)
				}
			}
			order, _ := svc.repo.BillingOrder(task.BillingOrderID)
			if order.Quantity != 62 || order.CostQuantity != 62 || order.ActualAmountMicrocredits != 155000 || order.ProviderRequestID != "rh-task" {
				t.Fatalf("recovered order used output duration: %#v", order)
			}
			var log model.ApiCallLog
			if err := db.First(&log, "task_id = ?", task.ID).Error; err != nil {
				t.Fatal(err)
			}
			var audit runninghub.Response
			if err := json.Unmarshal([]byte(log.ResponseBody), &audit); err != nil || audit.Usage.ConsumeMoney != "0.07" || audit.Usage.ConsumeCoins != float64(3) {
				t.Fatalf("cost audit lost: %#v, %v", audit, err)
			}
		})
	}
}

func TestRunningHubRefundedRecoveryDoesNotChargeOriginalVideoDuration(t *testing.T) {
	svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	if err := svc.taskBilling().RefundBilling(task.BillingOrderID, "failed"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); err != nil {
			t.Fatal(err)
		}
	}
	order, _ := svc.repo.BillingOrder(task.BillingOrderID)
	account, _ := svc.repo.CreditAccount(task.UserID)
	var consumed int64
	db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerConsume).Count(&consumed)
	if order.Status != model.BillingStatusSettled || order.Quantity != 121 || order.CostQuantity != 121 ||
		order.ActualAmountMicrocredits != 302500 || consumed != 1 || account.AvailableMicrocredits != 697500 || account.ReservedMicrocredits != 0 {
		t.Fatalf("old estimate charged during recovery: order=%#v account=%#v", order, account)
	}
	cost, err := billingCreditCost(*order)
	if err != nil || cost == nil || *cost != 201707 {
		t.Fatalf("restored runtime cost = %v, %v", cost, err)
	}
	records, err := svc.analyticsFinancialRecords([]model.ApiCallLog{
		{ID: "recovered", UserID: task.UserID, ChannelID: order.ChannelID, BillingOrderID: order.ID, RequestKind: "create", Billable: true},
		{ID: "poll", UserID: task.UserID, ChannelID: order.ChannelID, BillingOrderID: order.ID, RequestKind: "poll", Billable: true},
	})
	if err != nil || len(records) != 1 {
		t.Fatalf("restored financial records = %#v, %v", records, err)
	}
	finance := AnalyticsFinance{}
	finance.add(records["recovered"])
	if finance.RevenueMicrocredits != 302500 || finance.CostMicrocredits != 201707 || finance.ProfitMicrocredits == nil || *finance.ProfitMicrocredits != 100793 {
		t.Fatalf("restored cost/revenue/profit not based on runtime: %#v", finance)
	}
}

func TestRunningHubRefundedRecoveryFailsClosedWithoutRuntimeCapability(t *testing.T) {
	svc, _, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	order.Status = model.BillingStatusRefunded
	stub := &taskBillingRepositoryStub{order: order}
	svc.taskBillingCoordinator = newTaskBillingCoordinator(stub)
	if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); !errors.Is(err, errTaskBillingReview) {
		t.Fatalf("unsupported runtime recovery must await review: %v", err)
	}
	if len(stub.restoreCalls) != 0 || len(stub.settleCalls) != 0 {
		t.Fatalf("unsupported runtime recovery charged original amount: %#v", stub)
	}
}

func TestRunningHubRefundedRecoveryPreservesRefundOnInvalidEvidence(t *testing.T) {
	for _, test := range []struct {
		name, result string
		limit        bool
	}{
		{name: "missing usage", result: `{"video":{"durationMs":5000}}`},
		{name: "invalid runtime", result: `{"usage":{"taskCostTime":"NaN"}}`},
		{name: "charge limit exceeded", result: `{"usage":{"taskCostTime":120.1}}`, limit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
			if err := svc.taskBilling().RefundBilling(task.BillingOrderID, "failed"); err != nil {
				t.Fatal(err)
			}
			if test.limit {
				if err := db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).
					Updates(map[string]any{"charge_limit_set": true, "charge_limit_microcredits": 12500}).Error; err != nil {
					t.Fatal(err)
				}
			}
			task.ResultJSON = test.result
			if err := svc.settleRecoveredBilling(task, task.ProviderRequestID); err == nil {
				t.Fatal("unverified recovery unexpectedly succeeded")
			}
			order, _ := svc.repo.BillingOrder(task.BillingOrderID)
			account, _ := svc.repo.CreditAccount(task.UserID)
			var consumed int64
			db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerConsume).Count(&consumed)
			if order.Status != model.BillingStatusRefunded || order.Quantity != 5 || order.CostQuantity != 5 ||
				consumed != 0 || account.AvailableMicrocredits != 1_000_000 || account.ReservedMicrocredits != 0 {
				t.Fatalf("invalid recovery changed refund: order=%#v account=%#v", order, account)
			}
		})
	}
}

func TestRunningHubTerminalRefundsOnlyConfirmedUpstreamFailure(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELLED", "CANCELED", "RUNNING", "SUCCESS", "UNKNOWN", ""} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/context_cancelled=%t", status, cancelled), func(t *testing.T) {
				svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
				if err := db.Create(&model.ApiCallLog{ID: newID(), UserID: task.UserID, TaskID: task.ID, BillingOrderID: task.BillingOrderID,
					Billable: true, RequestKind: "create", Status: model.ApiCallStatusSucceeded}).Error; err != nil {
					t.Fatal(err)
				}
				if status != "" {
					addRunningHubBillingPoll(t, db, task, fmt.Sprintf(`{"taskId":"rh-task","status":%q}`, status))
				}
				// The worker's in-memory task predates the upstream ID checkpoint.
				task.ProviderRequestID = ""
				var failure error = errors.New("upstream failure text includes timeout")
				if cancelled {
					failure = context.Canceled
				}
				_ = runningHubTerminalForTest(svc, task).handleExecutionFailure(task, failure, false, false)
				order, _ := svc.repo.BillingOrder(task.BillingOrderID)
				account, _ := svc.repo.CreditAccount(task.UserID)
				confirmed := status == "FAILED" || status == "CANCELLED" || status == "CANCELED"
				if confirmed {
					if order.Status != model.BillingStatusRefunded || account.AvailableMicrocredits != 1_000_000 || account.ReservedMicrocredits != 0 {
						t.Fatalf("confirmed failure not refunded: %#v, %#v", order, account)
					}
				} else if order.Status != model.BillingStatusUncertain || account.ReservedMicrocredits != 12500 {
					t.Fatalf("unknown/success status refunded: %#v, %#v", order, account)
				}
			})
		}
	}
}

func TestRunningHubBillingRejectsUnrelatedOrStalePollEvidence(t *testing.T) {
	for _, change := range []string{"other provider task", "previous order", "different channel", "different user", "failed latest query", "latest running state"} {
		t.Run(change, func(t *testing.T) {
			svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
			task.ResultJSON = "{}"
			addRunningHubBillingPoll(t, db, task, `{"taskId":"rh-task","status":"SUCCESS","usage":{"taskCostTime":61}}`)
			switch change {
			case "other provider task":
				task.ProviderRequestID = "rh-other-task"
			case "previous order":
				db.Model(&model.ApiCallLog{}).Where("task_id = ?", task.ID).Update("billing_order_id", "old-order")
			case "different channel":
				db.Model(&model.ApiCallLog{}).Where("task_id = ?", task.ID).Update("channel_id", "old-channel")
			case "different user":
				db.Model(&model.ApiCallLog{}).Where("task_id = ?", task.ID).Update("user_id", "someone-else")
			case "failed latest query":
				db.Model(&model.ApiCallLog{}).Where("task_id = ?", task.ID).Update("status_code", 503)
			case "latest running state":
				addRunningHubBillingPoll(t, db, task, `{"taskId":"rh-task","status":"RUNNING"}`)
			}
			if handled, err := svc.settleRunningHubRuntimeBilling(*task, task.ProviderRequestID); !handled || err == nil {
				t.Fatalf("unrelated evidence settled: handled=%v err=%v", handled, err)
			}
			order, _ := svc.repo.BillingOrder(task.BillingOrderID)
			if order.Status != model.BillingStatusRunning || order.ActualAmountMicrocredits != 0 {
				t.Fatalf("invalid evidence changed billing: %#v", order)
			}
		})
	}
}

func TestRunningHubBillingDoesNotTrustTruncatedOldPollHistory(t *testing.T) {
	svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	task.ResultJSON = "{}"
	logs := make([]model.ApiCallLog, 1001)
	start := time.Now().Add(-time.Hour)
	for index := range logs {
		logs[index] = model.ApiCallLog{ID: fmt.Sprintf("poll-%04d", index), UserID: task.UserID, TaskID: task.ID, BillingOrderID: task.BillingOrderID,
			ChannelID: "rh-channel", Capability: "video", RequestKind: "poll", Path: runninghub.QueryPath, StatusCode: 200,
			ResponseBody: `{"taskId":"rh-task","status":"SUCCESS","usage":{"taskCostTime":1}}`, CreatedAt: start.Add(time.Duration(index) * time.Second)}
	}
	logs[1000].ResponseBody = `{"taskId":"rh-task","status":"RUNNING"}`
	if err := db.CreateInBatches(logs, 100).Error; err != nil {
		t.Fatal(err)
	}
	if handled, err := svc.settleRunningHubRuntimeBilling(*task, task.ProviderRequestID); !handled || err == nil {
		t.Fatalf("truncated history settled from stale evidence: %v, %v", handled, err)
	}
}

func TestRunningHubHourlyPriceUnits(t *testing.T) {
	// per_second stores integer microcredits/second, not credits/hour.
	// Except for Standard's sale price, the proposed hourly rates repeat in
	// base ten; these are the nearest representable per-second prices.
	for _, rate := range []struct {
		name                         string
		saleHourly, costHourly       int64
		salePerSecond, costPerSecond int64
	}{
		{"Standard", 9, 6, 2500, 1667},
		{"Plus", 12, 8, 3333, 2222},
		{"Ultra", 24, 16, 6667, 4444},
	} {
		t.Run(rate.name, func(t *testing.T) {
			sale, err := creditAmount(rate.salePerSecond, 3600, 10000)
			if err != nil || abs64(sale-rate.saleHourly*CreditScale) > 1800 {
				t.Fatalf("hourly sale unit mismatch: %d, %v", sale, err)
			}
			cost, err := creditAmount(rate.costPerSecond, 3600, 10000)
			if err != nil || abs64(cost-rate.costHourly*CreditScale) > 1800 {
				t.Fatalf("hourly cost unit mismatch: %d, %v", cost, err)
			}
		})
	}
}

func TestRunningHubBillingRejectsFixedPriceBeforeReservation(t *testing.T) {
	for _, mode := range []string{"fixed_request", "token"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, _ := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
			if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}); err != nil {
				t.Fatal(err)
			}
			channelModel := model.ChannelModel{ID: "rh-model", ChannelID: "rh-channel", ModelKey: runninghub.WorkflowID,
				Protocol: model.ChannelInterfaceRunningHubVideo, Capability: "video", Enabled: true, PriceConfigured: true}
			if err := db.Create(&channelModel).Error; err != nil {
				t.Fatal(err)
			}
			tier := model.ChannelModelPriceTier{ID: "rh-tier", ChannelModelID: channelModel.ID, SelectorJSON: `{}`, SelectorKey: "default",
				Enabled: true, PriceConfigured: true, BillingMode: mode, UnitPriceMicrocredits: 2500}
			if err := db.Create(&tier).Error; err != nil {
				t.Fatal(err)
			}
			order, err := svc.newBillingOrderWithPriceTier("runtime-user", "new-task", "new-order", "rh-channel", runninghub.WorkflowID,
				"video", "canvas_video", 5, tokenBillingEstimate{}, tier.ID)
			if err == nil || order != nil {
				t.Fatalf("invalid RunningHub sale mode accepted: %#v, %v", order, err)
			}
			var count int64
			if err := db.Model(&model.BillingOrder{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("rejected admission created an order: count=%d, %v", count, err)
			}
		})
	}
}

func TestRunningHubBillingRejectsUnifiedFixedPriceBeforeReservation(t *testing.T) {
	svc, db, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	if err := db.AutoMigrate(&model.LogicalModel{}, &model.LogicalModelRoute{}, &model.ChannelModel{}); err != nil {
		t.Fatal(err)
	}
	task.LogicalModelID, task.LogicalModelRevisionID, task.RouteID, task.ChannelModelID = "logical-rh", "revision-rh", "route-rh", "channel-rh"
	for _, row := range []any{
		&model.LogicalModel{ID: task.LogicalModelID, Enabled: true, Code: "rh-video", ActiveRevisionID: task.LogicalModelRevisionID, PricePolicy: "unified", BillingMode: "fixed_request"},
		&model.LogicalModelRoute{ID: task.RouteID, LogicalModelRevisionID: task.LogicalModelRevisionID, ChannelModelID: task.ChannelModelID},
		&model.ChannelModel{ID: task.ChannelModelID, Protocol: model.ChannelInterfaceRunningHubVideo, Enabled: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	order, err := svc.newLogicalModelBillingOrder(task.UserID, task, map[string]any{"mode": "video", "config": map[string]any{"videoSeconds": "5"}})
	if err == nil || order != nil {
		t.Fatalf("unified fixed price accepted: %#v, %v", order, err)
	}
}

func TestRunningHubTerminalReadFailureCannotSettleEstimate(t *testing.T) {
	svc, _, task := newRunningHubBillingFixture(t, 5, 1_000_000, 10000)
	terminal := runningHubTerminalForTest(svc, task)
	readErr := errors.New("read completed task failed")
	terminal.repo = &taskTerminalRepositoryStub{taskError: readErr}
	if err := terminal.handleSuccess(task); !errors.Is(err, readErr) {
		t.Fatalf("missing completed task settled: %v", err)
	}
	order, _ := svc.repo.BillingOrder(task.BillingOrderID)
	if order.Status != model.BillingStatusUncertain || order.ActualAmountMicrocredits != 0 {
		t.Fatalf("unreadable runtime settled by request duration: %#v", order)
	}
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
