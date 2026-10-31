package app

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestDramaTaskCostUsesSettledAmount(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	run := &dramaRun{s: s}
	for _, tc := range []struct {
		name   string
		status model.BillingStatus
		actual int64
		want   int64
	}{
		{"runtime supplement", model.BillingStatusSettled, 273306, 273306},
		{"runtime refund", model.BillingStatusSettled, 9999, 9999},
		{"free final charge", model.BillingStatusSettled, 0, 0},
		{"full refund", model.BillingStatusRefunded, 0, 0},
		{"pending reservation", model.BillingStatusRunning, 0, 16665},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := &model.BillingOrder{ID: tc.name, UserID: "user", TaskID: tc.name, IdempotencyKey: tc.name,
				Capability: "video", Status: tc.status, AmountMicrocredits: 16665, ActualAmountMicrocredits: tc.actual}
			if err := db.Create(order).Error; err != nil {
				t.Fatal(err)
			}
			cost, err := run.taskCost(&model.Task{BillingOrderID: order.ID})
			if err != nil || cost != tc.want {
				t.Fatalf("cost=%d want=%d err=%v", cost, tc.want, err)
			}
		})
	}
}
