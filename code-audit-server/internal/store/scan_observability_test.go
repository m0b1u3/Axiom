package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPostgresScanObservability(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	id, e := s.InsertPending(ctx, Task{ScanNo: "coverage-retention", SourcePath: "/src", Provider: "test", ScanMode: "deep", TimeoutMinutes: 3, DeepMaxDiscoveryRuns: 4, DeepStopAfterNoNew: 2})
	if e != nil {
		t.Fatal(e)
	}
	limits := ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2}
	task, e := s.ClaimExecution(ctx, "coverage-agent", limits)
	if e != nil || task == nil {
		t.Fatal(e)
	}
	if task.DeepMaxDiscoveryRuns != 4 || task.DeepStopAfterNoNew != 2 || task.TimeoutMinutes != 3 {
		t.Fatalf("options lost: %+v", task)
	}
	coverage := json.RawMessage(`{"completeness":"partial","surfaces":[],"deferred":[{"id":"auth","reason":"time limit"}],"future":{"keep":true}}`)
	progress := `{"phase":"partial","deep":{"completed":1,"active":0,"maximum":4},"reconnect":{"attempt":1,"reason":"network"}}`
	done := Completion{LeaseRef: LeaseRef{TaskID: id, Attempt: 1, AgentID: "coverage-agent"}, Status: "partial", Coverage: coverage, ProgressJSON: progress}
	if e = s.CompleteExecution(ctx, done); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteExecution(ctx, done); e != nil {
		t.Fatal("idempotent completion", e)
	}
	result, e := s.AttemptResult(ctx, id, 1)
	if e != nil || string(result.Coverage) != string(coverage) {
		t.Fatal("coverage lost", e, string(result.Coverage))
	}
	task, e = s.Get(ctx, id)
	if e != nil || task.ProgressJSON != progress || task.Status != "partial" {
		t.Fatal("final progress lost", e)
	}
	if _, e = s.Retry(ctx, id); e != nil {
		t.Fatal(e)
	}
	history, e := s.ListAttempts(ctx, id)
	if e != nil || history[0].ProgressJSON != progress {
		t.Fatal("historical progress lost", e)
	}
	task, e = s.Get(ctx, id)
	if e != nil || task.DeepMaxDiscoveryRuns != 4 {
		t.Fatal("retry lost policy", e)
	}
	result, e = s.AttemptResult(ctx, id, 1)
	if e != nil || string(result.Coverage) != string(coverage) {
		t.Fatal("retry lost coverage", e)
	}
}
