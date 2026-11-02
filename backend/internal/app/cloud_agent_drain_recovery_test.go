package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestCloudAgentDrainPreservesRunAndWallet(t *testing.T) {
	for _, path := range []string{"model bridge", "admission race"} {
		t.Run(path, func(t *testing.T) {
			s, _, root := approvalPauseRoot(t)
			run, state := agentInterjectionState(t, s, root.ID)
			s.BeginDrain()
			var err error
			if path == "model bridge" {
				_, _, err = s.runCloudAgentModelStep(context.Background(), "user", root.ID, []map[string]any{{"role": "user", "content": "continue"}}, "off")
			} else {
				err = s.enqueueCloudAgentTask(run, &state, creationTextRequest(), nil)
			}
			if !errors.Is(err, errTaskAdmissionDraining) {
				t.Fatalf("expected a typed maintenance pause, got %v", err)
			}
			after, afterState := agentInterjectionState(t, s, root.ID)
			if after.Status != run.Status || after.Revision != run.Revision || afterState.Step != state.Step || afterState.ActiveTaskID != "" {
				t.Fatalf("maintenance mutated run: status=%s revision=%d step=%d active=%s", after.Status, after.Revision, afterState.Step, afterState.ActiveTaskID)
			}
			account, err := s.repo.CreditAccount("user")
			if err != nil || account.AvailableMicrocredits != 10000 || account.ReservedMicrocredits != 0 {
				t.Fatalf("maintenance changed wallet: %+v (%v)", account, err)
			}
		})
	}
}

// Exercise the real Node runtime and model-task billing with a held fake
// upstream. Maintenance must preserve the completed tool round for recovery.
func TestCloudAgentRuntimeResumesToolRoundAfterDrain(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	upstream := &liveUpstream{}
	upstream.handler = func(call int, body map[string]any, w http.ResponseWriter) {
		if call == 1 {
			close(entered)
			<-release
			writeSSE(w, sseDelta(map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "drain-read", "type": "function",
				"function": map[string]any{"name": "canvas_get_state", "arguments": "{}"},
			}}}))
			return
		}
		writeSSE(w, sseDelta(map[string]any{"content": "Recovered tool round finished."}))
	}
	s, runID := startLiveAgentService(t, upstream, "read_only")
	defer unblock()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Agent did not reach the first model step")
	}
	s.BeginDrain()
	unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.StopWorker(ctx); err != nil {
		t.Fatalf("drain failed: %v", err)
	}
	execution, err := s.repo.CloudAgent("user", runID)
	if err != nil {
		t.Fatal(err)
	}
	if cloudAgentRunTerminal(execution.Status) {
		t.Fatalf("maintenance ended an unfinished tool round as %s: %q", execution.Status, execution.FailureMessage)
	}
	if calls := len(upstream.snapshot()); calls != 1 {
		t.Fatalf("drain started %d model calls; want only the existing step", calls)
	}
	session, err := s.repo.CloudAgentPiSession("user", runID)
	if err != nil || !strings.Contains(session.SessionJSONL, `"role":"toolResult"`) {
		t.Fatalf("tool result was not checkpointed before shutdown: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A fresh coordinator uses only the durable run and session, with no
	// in-memory runner state from the service that drained.
	restarted := New(s.repo, s.dataDir)
	t.Cleanup(func() { _ = restarted.Close() })
	done := make(chan struct{})
	workerDone := make(chan struct{})
	t.Cleanup(func() { close(done); <-workerDone })
	go func() {
		defer close(workerDone)
		for {
			select {
			case <-done:
				return
			default:
				_ = restarted.ProcessNextTask()
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	restarted.recoverCloudAgentPiRunners()
	execution = waitLiveAgentTerminal(t, restarted, runID)
	if execution.Status != "completed" {
		t.Fatalf("recovery status=%s failure=%q", execution.Status, execution.FailureMessage)
	}
	calls := upstream.snapshot()
	if len(calls) != 2 {
		t.Fatalf("recovery repeated a completed model step: calls=%d", len(calls))
	}
	results := upstreamToolMessages(calls[1].body)
	if len(results) != 1 || stringField(results[0], "tool_call_id") != "drain-read" || cloudAgentToolContentIsError(stringField(results[0], "content")) {
		t.Fatalf("recovery lost the successful tool result: %#v", results)
	}
	allTasks, err := restarted.repo.Tasks("user", 100, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var tasks []model.Task
	for _, task := range allTasks {
		if task.AgentRunID == runID && task.Operation == cloudAgentStepOperation {
			tasks = append(tasks, task)
		}
	}
	if len(tasks) != 2 {
		t.Fatalf("expected exactly two billed model steps, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Status != model.TaskStatusSucceeded || task.BillingOrderID == "" {
			t.Fatalf("step did not settle successfully: %s %s", task.ID, task.Status)
		}
		order, err := restarted.repo.BillingOrder(task.BillingOrderID)
		if err != nil || order.Status != model.BillingStatusSettled || order.ActualAmountMicrocredits != 100 {
			t.Fatalf("unexpected model-step settlement: %+v (%v)", order, err)
		}
	}
	account, err := restarted.repo.CreditAccount("user")
	if err != nil || account.AvailableMicrocredits != 9800 || account.ReservedMicrocredits != 0 {
		t.Fatalf("recovery charged more than two steps: %+v (%v)", account, err)
	}
	full, err := restarted.CloudAgentRun("user", runID)
	if err != nil {
		t.Fatal(err)
	}
	finalAnswers, toolExecutions := 0, 0
	for _, event := range full.Events {
		if event.Type == "assistant_message" && stringField(event.Payload, "text") == "Recovered tool round finished." {
			finalAnswers++
		}
		if event.Type == "tool_completed" && stringField(event.Payload, "callId") == "drain-read" {
			toolExecutions++
		}
	}
	if finalAnswers != 1 || toolExecutions != 1 {
		t.Fatalf("recovery repeated work: final answers=%d tool executions=%d", finalAnswers, toolExecutions)
	}
}
