package app

import (
	"context"
	"strings"
	"testing"

	"yingce/backend/internal/model"
)

func TestAgentMediaAdmissionPersistsOriginAndChargeAuthorization(t *testing.T) {
	s, _, args := agentMediaFixture(t)
	run, state := agentMediaRun(t, s, args, "auto")
	req, _, err := s.prepareCloudAgentMedia(run, &state, agentMediaCall(args))
	if err != nil {
		t.Fatal(err)
	}
	req.admission = &taskAdmission{ID: "generation-task", MaxCharge: 1000, AgentRunID: run.ID, GenerationID: "generation", ApprovalID: "approval"}
	req.creationPrepare = &creationTaskPreparation{}
	task, err := s.CreateTask(run.UserID, req)
	if err != nil {
		t.Fatal(err)
	}
	order := req.creationPrepare.Order
	if task.AgentRunID != run.ID || task.GenerationID != "generation" || task.ApprovalID != "approval" || order == nil || !order.ChargeLimitSet || task.AuthorizedChargeMicrocredits != order.AmountMicrocredits || order.ChargeLimitMicrocredits != order.AmountMicrocredits {
		t.Fatalf("incomplete generation authorization: task=%+v order=%+v", task, order)
	}
}

func TestAgentRunningHubVideoUsesRemainingBudgetAsChargeLimit(t *testing.T) {
	order := &model.BillingOrder{AmountMicrocredits: 25_000, Capability: "video", BillingMode: "per_second"}
	admission := &taskAdmission{MaxCharge: 200_000_000}
	input := map[string]any{"config": map[string]any{"interfaceType": string(model.ChannelInterfaceRunningHubVideo)}}
	if got := agentChargeLimitMicrocredits(order, admission, input); got != admission.MaxCharge {
		t.Fatalf("RunningHub Agent video charge limit = %d, want remaining budget %d", got, admission.MaxCharge)
	}
	if got := agentChargeLimitMicrocredits(order, admission, map[string]any{"config": map[string]any{"interfaceType": "volcengine-ark-video"}}); got != order.AmountMicrocredits {
		t.Fatalf("non-RunningHub Agent video charge limit = %d, want quote %d", got, order.AmountMicrocredits)
	}
}

func TestCloudAgentBudgetUsesSettledActualCharge(t *testing.T) {
	orders := []model.BillingOrder{
		{Status: model.BillingStatusSettled, AmountMicrocredits: 25_000, ActualAmountMicrocredits: 540_000},
		{Status: model.BillingStatusReserved, AmountMicrocredits: 25_000, ChargeLimitSet: true, ChargeLimitMicrocredits: 200_000_000},
	}
	used := int64(0)
	for _, order := range orders {
		used += cloudAgentOrderBudgetUsage(order)
	}
	if used != 200_540_000 {
		t.Fatalf("Agent budget usage = %d, want settled actual plus active authorization 200540000", used)
	}
}

func TestAgentMediaRetryRequiresNewGenerationAuthorization(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	for _, task := range []model.Task{
		{ID: "agent-run-only", AgentRunID: "run"},
		{ID: "generation-only", GenerationID: "generation"},
		{ID: "operation-prefix", Operation: "cloud_agent_media"},
		{ID: "source", InputJSON: `{"metadata":{"source":"cloud_agent"}}`},
	} {
		task.UserID, task.Type, task.Status = "user", "canvas_video", model.TaskStatusFailed
		if task.Operation == "" {
			task.Operation = "image_to_video"
		}
		if task.InputJSON == "" {
			task.InputJSON = `{}`
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.RetryTask(task.UserID, task.ID); err == nil || !strings.Contains(err.Error(), "Agent 重试") {
			t.Fatalf("media retry retained old authorization: %v", err)
		}
		stored, err := s.repo.Task(task.ID)
		if err != nil || stored.Status != model.TaskStatusFailed || stored.BillingOrderID != "" {
			t.Fatalf("retry changed old execution: %+v %v", stored, err)
		}
	}
}

func TestOrdinaryFailedAndCancelledTasksKeepRetryIdentity(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	for _, status := range []model.TaskStatus{model.TaskStatusFailed, model.TaskStatusCancelled} {
		task := model.Task{
			ID:        "ordinary-" + string(status),
			UserID:    "user",
			Type:      "canvas_text",
			Operation: "text",
			Status:    status,
			InputJSON: `{}`,
			RouteRun:  3,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}

		retried, err := s.RetryTask(task.UserID, task.ID)
		if err != nil {
			t.Fatalf("ordinary %s task should remain retryable: %v", status, err)
		}
		if retried.ID != task.ID || retried.Status != model.TaskStatusQueued || retried.RouteRun != 4 || retried.GenerationID != "" || retried.AgentRunID != "" {
			t.Fatalf("retry must reuse task execution identity without inventing an Agent generation: %+v", retried)
		}
		if _, err = s.RetryTask(task.UserID, task.ID); err == nil {
			t.Fatal("queued retry was accepted a second time")
		}
	}
}

func TestTaskCancellationPersistsFirstSourceAndDiagnostic(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	task := model.Task{ID: "cancel-child", UserID: "user", AgentRunID: "parent", Status: model.TaskStatusQueued}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.taskLifecycle().cancelTaskWithIntent(context.Background(), "user", task.ID, model.TaskCancellationIntent{Source: model.TaskCancellationParentFailed}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelTask(context.Background(), "user", task.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := taskExecutionDiagnostic(stored)
	if stored.CancellationSource != model.TaskCancellationParentFailed || stored.CancellationRequestedAt == nil || diagnostic == nil || diagnostic.Phase != "cancellation" || diagnostic.SubmissionOutcome != "not_submitted" || diagnostic.TaskSubmitted {
		t.Fatalf("cancellation lost facts: %+v %+v", stored, diagnostic)
	}
}
