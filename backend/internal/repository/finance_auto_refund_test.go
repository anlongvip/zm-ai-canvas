package repository

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestAutoRefundableBillingOrdersRequiresTerminalTask(t *testing.T) {
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

	items, err := repo.AutoRefundableBillingOrders(10, nil)
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
	if !got["order-failed"] {
		t.Fatalf("eligible uncertain order was omitted: %#v", items)
	}
	// 上游 v1.8.3 起只自动退回能证明已终止的通用任务订单：没有 tasks 行的订单可能属于
	// 电商等独立运行域，缺少任务行不能当作停止执行的证据，因此不得进入自动退款队列。
	if got["order-orphan"] {
		t.Fatalf("order without a task row must not be auto refunded: %#v", items)
	}
}
