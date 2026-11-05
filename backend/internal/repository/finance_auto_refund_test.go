package repository

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestAutoRefundableBillingOrdersExcludesSucceededTasks(t *testing.T) {
	repo, db := newRefundedBillingRecoveryRepository(t)
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	orders := []model.BillingOrder{
		{ID: "order-succeeded", UserID: "user-1", IdempotencyKey: "key-succeeded", TaskID: "task-succeeded", Status: model.BillingStatusUncertain, AmountMicrocredits: 100},
		{ID: "order-failed", UserID: "user-1", IdempotencyKey: "key-failed", TaskID: "task-failed", Status: model.BillingStatusUncertain, AmountMicrocredits: 100},
		{ID: "order-orphan", UserID: "user-1", IdempotencyKey: "key-orphan", Status: model.BillingStatusUncertain, AmountMicrocredits: 100},
	}
	for i := range orders {
		if err := db.Create(&orders[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []model.Task{
		{ID: "task-succeeded", UserID: "user-1", BillingOrderID: "order-succeeded", Status: model.TaskStatusSucceeded},
		{ID: "task-failed", UserID: "user-1", BillingOrderID: "order-failed", Status: model.TaskStatusFailed},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.AutoRefundableBillingOrders(10)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(items))
	for _, item := range items {
		got[item.ID] = true
	}
	if got["order-succeeded"] {
		t.Fatalf("succeeded task was returned for automatic refund: %#v", items)
	}
	if !got["order-failed"] || !got["order-orphan"] {
		t.Fatalf("eligible uncertain orders were omitted: %#v", items)
	}
}
