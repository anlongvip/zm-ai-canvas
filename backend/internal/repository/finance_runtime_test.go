package repository

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestVideoRuntimeSettlementRollsBackFundsWhenLedgerFails(t *testing.T) {
	for _, test := range []struct {
		name       string
		durationMs int64
		failType   model.CreditLedgerType
	}{
		{name: "supplement consume ledger", durationMs: 120100, failType: model.CreditLedgerConsume},
		{name: "refund ledger after consume", durationMs: 2100, failType: model.CreditLedgerRefund},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, db := newRefundedBillingRecoveryRepository(t)
			if err := db.Create(&model.CreditAccount{UserID: "runtime-user", AvailableMicrocredits: 1_000_000}).Error; err != nil {
				t.Fatal(err)
			}
			order := &model.BillingOrder{
				ID: "runtime-order", UserID: "runtime-user", IdempotencyKey: "runtime-task", Capability: "video", BillingMode: "per_second",
				Status: model.BillingStatusRunning, Quantity: 5, AmountMicrocredits: 12500, ReservedAmountMicrocredits: 12500,
				UnitPriceMicrocredits: 2500, MultiplierBasisPoints: 10000,
				BillingCostSnapshot: model.BillingCostSnapshot{CostBillingMode: "per_second", CostQuantity: 5},
			}
			if err := repo.ReserveBillingOrder(order); err != nil {
				t.Fatal(err)
			}
			ledgerErr := errors.New("ledger persistence unavailable")
			if err := db.Callback().Create().Before("gorm:create").Register("runtime-ledger-failure", func(tx *gorm.DB) {
				if entry, ok := tx.Statement.Dest.(*model.CreditLedgerEntry); ok && entry.Type == test.failType {
					tx.AddError(ledgerErr)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SettleBillingOrderWithAudioDuration(order.ID, "rh-task", test.durationMs); !errors.Is(err, ledgerErr) {
				t.Fatalf("expected ledger failure, got %v", err)
			}
			after, err := repo.BillingOrder(order.ID)
			if err != nil {
				t.Fatal(err)
			}
			account, err := repo.CreditAccount(order.UserID)
			if err != nil {
				t.Fatal(err)
			}
			var entries int64
			if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&entries).Error; err != nil {
				t.Fatal(err)
			}
			if after.Status != model.BillingStatusRunning || after.Quantity != 5 || after.CostQuantity != 5 || after.ActualAmountMicrocredits != 0 ||
				account.AvailableMicrocredits != 987500 || account.ReservedMicrocredits != 12500 || entries != 1 {
				t.Fatalf("partial runtime settlement: order=%#v account=%#v ledger entries=%d", after, account, entries)
			}
			if err := db.Callback().Create().Remove("runtime-ledger-failure"); err != nil {
				t.Fatal(err)
			}
			// Once persistence recovers, the same reservation can settle once.
			for range 2 {
				if err := repo.SettleBillingOrderWithAudioDuration(order.ID, "rh-task", test.durationMs); err != nil {
					t.Fatal(err)
				}
			}
			account, _ = repo.CreditAccount(order.UserID)
			after, _ = repo.BillingOrder(order.ID)
			if account.AvailableMicrocredits != 1_000_000-after.ActualAmountMicrocredits || account.ReservedMicrocredits != 0 || after.Status != model.BillingStatusSettled {
				t.Fatalf("retry did not conserve funds: order=%#v account=%#v", after, account)
			}
		})
	}
}

func newRefundedRuntimeOrder(t *testing.T) (*Repository, *gorm.DB, *model.BillingOrder) {
	t.Helper()
	repo, db := newRefundedBillingRecoveryRepository(t)
	if err := db.Create(&model.CreditAccount{UserID: "runtime-user", AvailableMicrocredits: 1_000_000, ReservedMicrocredits: 7000}).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: "runtime-order", UserID: "runtime-user", IdempotencyKey: "runtime-task", TaskID: "runtime-task",
		Capability: "video", BillingMode: "per_second", Quantity: 5, AmountMicrocredits: 12500,
		UnitPriceMicrocredits: 2500, MultiplierBasisPoints: 10000, ProviderRequestID: "rh-task",
		BillingCostSnapshot: model.BillingCostSnapshot{CostBillingMode: "per_second", CostQuantity: 5,
			CostPricing: model.CreditCostPricing{Configured: true, UnitPriceMicrocredits: 1667}},
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	if err := repo.RefundBillingOrder(order.ID, "upstream outcome was not yet known"); err != nil {
		t.Fatal(err)
	}
	return repo, db, order
}

func assertRefundedRuntimeCharge(t *testing.T, repo *Repository, db *gorm.DB, actual, quantity, available int64) {
	t.Helper()
	order, err := repo.BillingOrder("runtime-order")
	if err != nil {
		t.Fatal(err)
	}
	account, err := repo.CreditAccount(order.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.Quantity != quantity || order.CostQuantity != quantity ||
		order.ActualAmountMicrocredits != actual || order.AmountMicrocredits != 12500 || order.ReservedAmountMicrocredits != 12500 ||
		order.RefundedAmountMicrocredits != max(12500-actual, 0) || order.ProviderRequestID != "rh-task" ||
		order.RefundedAt != nil || order.SettledAt == nil || order.Error != "" ||
		order.UnitPriceMicrocredits != 2500 || order.CostPricing.UnitPriceMicrocredits != 1667 ||
		account.AvailableMicrocredits != available-actual || account.ReservedMicrocredits != 7000 {
		t.Fatalf("incorrect runtime recovery: order=%#v account=%#v", order, account)
	}
	var entries []model.CreditLedgerEntry
	if err := db.Where("billing_order_id = ?", order.ID).Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	consumes := 0
	var availableDelta, reservedDelta int64
	for _, entry := range entries {
		availableDelta += entry.AvailableDeltaMicrocredits
		reservedDelta += entry.ReservedDeltaMicrocredits
		if entry.Type == model.CreditLedgerConsume {
			consumes++
			if entry.AmountMicrocredits != -actual || entry.AvailableDeltaMicrocredits != -actual || entry.ReservedDeltaMicrocredits != 0 ||
				entry.AvailableAfterMicrocredits != available-actual || entry.ReservedAfterMicrocredits != 7000 {
				t.Fatalf("incorrect recovery consume ledger: %#v", entry)
			}
		}
	}
	if len(entries) != 3 || consumes != 1 || availableDelta != -actual || reservedDelta != 0 {
		t.Fatalf("recovery ledger does not reconcile: %#v", entries)
	}
}

func TestRestoreRefundedVideoRuntimeChargesActualOnce(t *testing.T) {
	for _, test := range []struct {
		name                                              string
		duration, quantity, multiplier, actual, available int64
	}{
		{"shorter than reservation", 2100, 3, 10000, 7500, 1_000_000},
		{"longer than reservation", 120100, 121, 10000, 302500, 1_000_000},
		{"sale multiplier only", 120100, 121, 12500, 378125, 1_000_000},
		{"exact second", 120000, 120, 10000, 300000, 1_000_000},
		{"subsecond", 1, 1, 10000, 2500, 1_000_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			if err := db.Model(order).Update("multiplier_basis_points", test.multiplier).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", test.available).Error; err != nil {
				t.Fatal(err)
			}
			before, _ := repo.CreditAccount(order.UserID)
			for range 3 {
				if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", test.duration); err != nil {
					t.Fatal(err)
				}
			}
			assertRefundedRuntimeCharge(t, repo, db, test.actual, test.quantity, test.available)
			after, _ := repo.CreditAccount(order.UserID)
			if after.Version != before.Version+1 {
				t.Fatalf("repeated recovery modified account: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestRestoreRefundedVideoRuntimeRejectsInvalidRecovery(t *testing.T) {
	for _, test := range []struct {
		name           string
		duration       int64
		provider       string
		changes        map[string]any
		balance        int64
		missingAccount bool
	}{
		{name: "zero runtime", provider: "rh-task"},
		{name: "negative runtime", duration: -1, provider: "rh-task"},
		{name: "runtime overflow", duration: math.MaxInt64, provider: "rh-task"},
		{name: "missing provider", duration: 120100},
		{name: "mismatched provider", duration: 120100, provider: "other-task"},
		{name: "wrong capability", duration: 120100, provider: "rh-task", changes: map[string]any{"capability": "audio"}},
		{name: "wrong billing mode", duration: 120100, provider: "rh-task", changes: map[string]any{"billing_mode": "fixed_request"}},
		{name: "wrong cost mode", duration: 120100, provider: "rh-task", changes: map[string]any{"cost_billing_mode": "fixed_request"}},
		{name: "negative price", duration: 120100, provider: "rh-task", changes: map[string]any{"unit_price_microcredits": -1}},
		{name: "sale amount overflow", duration: 120100, provider: "rh-task", changes: map[string]any{"unit_price_microcredits": int64(math.MaxInt64)}},
		{name: "cost amount overflow", duration: 120100, provider: "rh-task", changes: map[string]any{"cost_unit_price_microcredits": int64(math.MaxInt64)}},
		{name: "negative cost", duration: 120100, provider: "rh-task", changes: map[string]any{"cost_unit_price_microcredits": -1}},
		{name: "invalid multiplier", duration: 120100, provider: "rh-task", changes: map[string]any{"multiplier_basis_points": 0}},
		{name: "charge limit exceeded", duration: 120100, provider: "rh-task", changes: map[string]any{"charge_limit_set": true, "charge_limit_microcredits": 12500}},
		{name: "zero authorization", duration: 120100, provider: "rh-task", changes: map[string]any{"charge_limit_set": true, "charge_limit_microcredits": 0}},
		{name: "running is not refunded", duration: 120100, provider: "rh-task", changes: map[string]any{"status": model.BillingStatusRunning}},
		{name: "uncertain reservation is not refunded", duration: 120100, provider: "rh-task", changes: map[string]any{"status": model.BillingStatusUncertain, "refunded_at": nil}},
		{name: "missing account", duration: 120100, provider: "rh-task", missingAccount: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			if len(test.changes) > 0 {
				if err := db.Model(order).Updates(test.changes).Error; err != nil {
					t.Fatal(err)
				}
			}
			if test.balance != 0 {
				if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", test.balance).Error; err != nil {
					t.Fatal(err)
				}
			}
			if test.missingAccount {
				if err := db.Where("user_id = ?", order.UserID).Delete(&model.CreditAccount{}).Error; err != nil {
					t.Fatal(err)
				}
			}
			beforeOrder, _ := repo.BillingOrder(order.ID)
			var beforeAccount, afterAccount model.CreditAccount
			db.Where("user_id = ?", order.UserID).Find(&beforeAccount)
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, test.provider, test.duration); err == nil {
				t.Fatal("invalid recovery unexpectedly succeeded")
			}
			afterOrder, _ := repo.BillingOrder(order.ID)
			db.Where("user_id = ?", order.UserID).Find(&afterAccount)
			var entries int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&entries)
			if !reflect.DeepEqual(beforeOrder, afterOrder) || !reflect.DeepEqual(beforeAccount, afterAccount) || entries != 2 {
				t.Fatalf("rejected recovery changed persisted funds/snapshot: before=%#v after=%#v account=%#v entries=%d", beforeOrder, afterOrder, afterAccount, entries)
			}
		})
	}
}

func TestRestoreRefundedVideoRuntimeRollsBackEveryWrite(t *testing.T) {
	for _, stage := range []string{"account", "order", "ledger"} {
		t.Run(stage, func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			beforeOrder, _ := repo.BillingOrder(order.ID)
			beforeAccount, _ := repo.CreditAccount(order.UserID)
			persistErr := errors.New("injected persistence failure")
			fail := func(tx *gorm.DB) {
				if (stage == "account" && tx.Statement.Table == "credit_accounts") ||
					(stage == "order" && tx.Statement.Table == "billing_orders") ||
					(stage == "ledger" && tx.Statement.Table == "credit_ledger_entries") {
					tx.AddError(persistErr)
				}
			}
			if stage == "ledger" {
				if err := db.Callback().Create().Before("gorm:create").Register("runtime-recovery-failure", fail); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Callback().Update().Before("gorm:update").Register("runtime-recovery-failure", fail); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, persistErr) {
				t.Fatalf("expected injected failure, got %v", err)
			}
			afterOrder, _ := repo.BillingOrder(order.ID)
			afterAccount, _ := repo.CreditAccount(order.UserID)
			var entries int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&entries)
			if !reflect.DeepEqual(beforeOrder, afterOrder) || !reflect.DeepEqual(beforeAccount, afterAccount) || entries != 2 {
				t.Fatalf("partial recovery persisted after %s failure: order=%#v account=%#v ledger=%d", stage, afterOrder, afterAccount, entries)
			}
			if stage == "ledger" {
				if err := db.Callback().Create().Remove("runtime-recovery-failure"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Callback().Update().Remove("runtime-recovery-failure"); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); err != nil {
					t.Fatal(err)
				}
			}
			assertRefundedRuntimeCharge(t, repo, db, 302500, 121, 1_000_000)
		})
	}
}

func TestRestoreRefundedVideoRuntimeConcurrentRequests(t *testing.T) {
	repo, db, order := newRefundedRuntimeOrder(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite serializes writers. Exercise concurrent callers over one writer
	// connection; PostgreSQL additionally uses SELECT FOR UPDATE in production.
	sqlDB.SetMaxOpenConns(1)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertRefundedRuntimeCharge(t, repo, db, 302500, 121, 1_000_000)
}

func TestRestoreRefundedVideoRuntimeInsufficientCreditsRequiresReview(t *testing.T) {
	for _, balance := range []int64{100, -100} {
		t.Run(fmt.Sprintf("balance=%d", balance), func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			// Another task's held credits must never fund a refunded recovery.
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).
				Updates(map[string]any{"available_microcredits": balance, "reserved_microcredits": 500000}).Error; err != nil {
				t.Fatal(err)
			}
			before, _ := repo.CreditAccount(order.UserID)
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrInsufficientCredits) {
				t.Fatalf("expected balance review, got %v", err)
			}
			after, _ := repo.CreditAccount(order.UserID)
			review, _ := repo.BillingOrder(order.ID)
			if !reflect.DeepEqual(before, after) || review.Status != model.BillingStatusUncertain || review.RefundedAt == nil ||
				review.Quantity != 121 || review.CostQuantity != 121 || review.ActualAmountMicrocredits != 0 || review.RefundedAmountMicrocredits != 12500 ||
				review.ProviderRequestID != "rh-task" || review.Error == "" || review.SettledAt != nil {
				t.Fatalf("insufficient recovery not safely audited: order=%#v account=%#v", review, after)
			}
			for _, retry := range []func() error{
				func() error { return repo.SettleBillingOrderWithAudioDuration(order.ID, "rh-task", 120100) },
				func() error { return repo.SettleBillingOrder(order.ID, "rh-task") },
				func() error { return repo.RefundBillingOrder(order.ID, "retry refund") },
			} {
				if err := retry(); !errors.Is(err, ErrBillingStateConflict) {
					t.Fatalf("released reservation reused: %v", err)
				}
			}
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrInsufficientCredits) {
				t.Fatalf("review retry should still require available funds: %v", err)
			}
			final, _ := repo.CreditAccount(order.UserID)
			var count int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&count)
			if !reflect.DeepEqual(before, final) || count != 2 {
				t.Fatalf("review retry changed funds: %#v ledger=%d", final, count)
			}
		})
	}
}

func TestRestoreRefundedVideoRuntimeReviewCanBeResolvedWithoutOtherReservation(t *testing.T) {
	for _, action := range []string{"settle", "refund"} {
		t.Run(action, func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			other := &model.BillingOrder{ID: "other-order", UserID: order.UserID, IdempotencyKey: "other-task", Status: model.BillingStatusReserved, Capability: "image", BillingMode: "fixed_request", AmountMicrocredits: 500000, Quantity: 1}
			if err := repo.ReserveBillingOrder(other); err != nil {
				t.Fatal(err)
			}
			otherBefore, _ := repo.BillingOrder(other.ID)
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", 100).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrInsufficientCredits) {
				t.Fatal(err)
			}
			before, _ := repo.CreditAccount(order.UserID)
			if action == "settle" {
				// A different runtime cannot replace the evidence already reviewed.
				if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 1000); !errors.Is(err, ErrBillingStateConflict) {
					t.Fatalf("review runtime was replaced: %v", err)
				}
				if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", 400000).Error; err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				for range 2 {
					if err := repo.CloseRefundedRuntimeBillingReview(order.ID, "admin", "retain confirmed refund"); err != nil {
						t.Fatal(err)
					}
				}
				if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrBillingStateConflict) {
					t.Fatalf("resolved refund was reopened: %v", err)
				}
			}
			after, _ := repo.CreditAccount(order.UserID)
			resolved, _ := repo.BillingOrder(order.ID)
			otherAfter, _ := repo.BillingOrder(other.ID)
			var entries []model.CreditLedgerEntry
			if err := db.Where("billing_order_id = ?", order.ID).Find(&entries).Error; err != nil {
				t.Fatal(err)
			}
			if after.ReservedMicrocredits != 507000 || !reflect.DeepEqual(otherBefore, otherAfter) {
				t.Fatalf("another order's reservation changed: account=%#v order=%#v", after, otherAfter)
			}
			if action == "settle" {
				if resolved.Status != model.BillingStatusSettled || resolved.Quantity != 121 || resolved.CostQuantity != 121 || resolved.ActualAmountMicrocredits != 302500 ||
					after.AvailableMicrocredits != 97500 || after.Version != before.Version+1 || len(entries) != 3 || resolved.RefundedAt != nil || resolved.SettledAt == nil {
					t.Fatalf("review settlement failed: %#v %#v entries=%#v", resolved, after, entries)
				}
			} else if resolved.Status != model.BillingStatusRefunded || resolved.ResolvedBy != "admin" || resolved.ResolutionNote == "" || resolved.RefundedAt == nil ||
				resolved.ActualAmountMicrocredits != 0 || resolved.RefundedAmountMicrocredits != 12500 || !reflect.DeepEqual(before, after) || len(entries) != 2 {
				t.Fatalf("closing review moved funds: %#v %#v entries=%#v", resolved, after, entries)
			}
		})
	}
}

func TestRestoreRefundedVideoRuntimeReviewResolutionRollsBack(t *testing.T) {
	for _, stage := range []string{"account", "order", "ledger", "close review"} {
		t.Run(stage, func(t *testing.T) {
			repo, db, order := newRefundedRuntimeOrder(t)
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", 100).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrInsufficientCredits) {
				t.Fatal(err)
			}
			if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", 400000).Error; err != nil {
				t.Fatal(err)
			}
			beforeOrder, _ := repo.BillingOrder(order.ID)
			beforeAccount, _ := repo.CreditAccount(order.UserID)
			persistErr := errors.New("review resolution persistence unavailable")
			fail := func(tx *gorm.DB) {
				if (stage == "account" && tx.Statement.Table == "credit_accounts") ||
					((stage == "order" || stage == "close review") && tx.Statement.Table == "billing_orders") ||
					(stage == "ledger" && tx.Statement.Table == "credit_ledger_entries") {
					tx.AddError(persistErr)
				}
			}
			if stage == "ledger" {
				if err := db.Callback().Create().Before("gorm:create").Register("review-resolution-failure", fail); err != nil {
					t.Fatal(err)
				}
			} else if err := db.Callback().Update().Before("gorm:update").Register("review-resolution-failure", fail); err != nil {
				t.Fatal(err)
			}
			var err error
			if stage == "close review" {
				err = repo.CloseRefundedRuntimeBillingReview(order.ID, "admin", "retain refund")
			} else {
				err = repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100)
			}
			if !errors.Is(err, persistErr) {
				t.Fatalf("expected write failure, got %v", err)
			}
			afterOrder, _ := repo.BillingOrder(order.ID)
			afterAccount, _ := repo.CreditAccount(order.UserID)
			var entries int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&entries)
			if !reflect.DeepEqual(beforeOrder, afterOrder) || !reflect.DeepEqual(beforeAccount, afterAccount) || entries != 2 {
				t.Fatalf("review resolution partially committed: %#v %#v entries=%d", afterOrder, afterAccount, entries)
			}
		})
	}
}

func TestRestoreRefundedVideoRuntimeReviewWriteFailureRollsBack(t *testing.T) {
	repo, db, order := newRefundedRuntimeOrder(t)
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Update("available_microcredits", 100).Error; err != nil {
		t.Fatal(err)
	}
	beforeOrder, _ := repo.BillingOrder(order.ID)
	beforeAccount, _ := repo.CreditAccount(order.UserID)
	persistErr := errors.New("review persistence unavailable")
	if err := db.Callback().Update().Before("gorm:update").Register("runtime-review-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_orders" {
			tx.AddError(persistErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, persistErr) {
		t.Fatalf("expected review write error: %v", err)
	}
	afterOrder, _ := repo.BillingOrder(order.ID)
	afterAccount, _ := repo.CreditAccount(order.UserID)
	if !reflect.DeepEqual(beforeOrder, afterOrder) || !reflect.DeepEqual(beforeAccount, afterAccount) {
		t.Fatalf("review failure partially persisted: %#v %#v", afterOrder, afterAccount)
	}
	if err := db.Callback().Update().Remove("runtime-review-failure"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RestoreRefundedBillingOrderWithRuntime(order.ID, "rh-task", 120100); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("review retry failed: %v", err)
	}
	review, _ := repo.BillingOrder(order.ID)
	if review.Status != model.BillingStatusUncertain {
		t.Fatalf("review retry did not persist uncertain: %#v", review)
	}
}
