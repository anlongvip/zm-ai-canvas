package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartCloudAgentPiDoesNotRestartAnActiveRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Service{piRunners: map[string]context.CancelFunc{"run-a": cancel}}
	s.startCloudAgentPi("user-a", "run-a")

	if err := ctx.Err(); err != nil {
		t.Fatalf("duplicate start cancelled the active Pi runner: %v", err)
	}
}

func TestStopWorkerWaitsForAgentFinalCheckpoint(t *testing.T) {
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	s := &Service{piRunners: map[string]context.CancelFunc{"run-a": cancelRunner}}
	s.piRunnerWg.Add(1)
	release := make(chan struct{})
	checkpoint := make(chan struct{})
	go func() {
		defer s.piRunnerWg.Done()
		<-release
		close(checkpoint)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- s.StopWorker(ctx) }()
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("shutdown returned before Agent checkpoint: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if runnerCtx.Err() != nil {
		close(release)
		t.Fatal("shutdown cancelled the Agent before its final checkpoint")
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	select {
	case <-checkpoint:
	default:
		t.Fatal("Agent checkpoint not complete at shutdown")
	}
	s.piRunnerMu.Lock()
	closed := s.piRunnersClosed
	s.piRunnerMu.Unlock()
	if !closed {
		t.Fatal("shutdown still admits new Agent sessions")
	}
}

func TestStopWorkerReportsAgentDrainTimeout(t *testing.T) {
	s := &Service{}
	s.piRunnerWg.Add(1)
	defer s.piRunnerWg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.StopWorker(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("unfinished Agent shutdown = %v, want cancellation", err)
	}
}

// 审批恢复时旧会话可能还在收尾：不能取消它，也不能丢弃恢复请求。
func TestResumeCloudAgentPiQueuesRestartWhileRunnerIsExiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Service{piRunners: map[string]context.CancelFunc{"run-a": cancel}}
	s.resumeCloudAgentPi("user-a", "run-a")

	if err := ctx.Err(); err != nil {
		t.Fatalf("resume cancelled the exiting Pi runner: %v", err)
	}
	if _, queued := s.piRunnerRestarts["run-a"]; !queued {
		t.Fatal("resume request was dropped while the previous runner was still exiting")
	}
}
