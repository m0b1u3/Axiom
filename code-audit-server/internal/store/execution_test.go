package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func pgStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL unset")
	}
	if !strings.Contains(url, "/axiom_test") {
		t.Fatal("use dedicated axiom_test database")
	}
	s, e := Open(url)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	_, e = s.db.ExecContext(context.Background(), "TRUNCATE knowledge_versions,knowledge_documents,storage_cleanup_jobs,storage_settings,auth_sessions,login_limits,operation_audits,execution_settings,execution_setting_changes,execution_agents,scan_attempts,findings,scan_tasks,source_snapshots,model_configs,projects,users RESTART IDENTITY CASCADE")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestPostgresExecution(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	limits := ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2}
	for i := 0; i < 3; i++ {
		if _, e := s.InsertPending(ctx, Task{ScanNo: fmt.Sprint("queue-", i), SourcePath: "/src", Provider: "deepseek", Model: "m", ScanMode: "standard"}); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	claimed := make(chan *Task, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, e := s.ClaimExecution(ctx, fmt.Sprint("a-", i), limits)
			if e != nil {
				errs <- e
			}
			if task != nil {
				claimed <- task
			}
		}(i)
	}
	wg.Wait()
	close(claimed)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d, want 2", len(claimed))
	}
	a, b := <-claimed, <-claimed
	if a.ID == b.ID || a.OutputPath == b.OutputPath {
		t.Fatal("execution collision")
	}
	ref := LeaseRef{TaskID: a.ID, Attempt: a.Attempt, AgentID: a.AgentID}
	if _, e := s.HeartbeatExecution(ctx, LeaseRef{TaskID: a.ID, Attempt: 0, AgentID: a.AgentID}, time.Minute, "", ""); !errors.Is(e, ErrLeaseLost) {
		t.Fatalf("old heartbeat accepted %v", e)
	}
	if _, e := s.HeartbeatExecution(ctx, ref, time.Minute, "discovery", `{"phase":"discovery"}`); e != nil {
		t.Fatal(e)
	}
	// Duplicate fingerprints force rollback: task must remain running and no partial writes survive.
	done := Completion{LeaseRef: ref, Status: "completed", Findings: []Finding{{Fingerprint: "fp", Title: "test", Severity: "high", Description: "test"}, {Fingerprint: "fp", Title: "duplicate", Severity: "high", Description: "test"}}}
	if e := s.CompleteExecution(ctx, done); e == nil {
		t.Fatal("invalid result accepted")
	}
	task, _ := s.Get(ctx, a.ID)
	if task.Status != "scanning" || task.FindingCount != 0 {
		t.Fatalf("non-atomic completion %+v", task)
	}
	done.Findings = done.Findings[:1]
	if e := s.CompleteExecution(ctx, done); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteExecution(ctx, done); e != nil {
		t.Fatalf("idempotence %v", e)
	}
	if ok, e := s.Retry(ctx, a.ID); e != nil || !ok {
		t.Fatal("retry", e)
	}
	task, _ = s.Get(ctx, a.ID)
	if task.StartedAt != "" {
		t.Fatal("retry start time retained")
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE scan_tasks SET queued_at=NOW()+INTERVAL '1 hour' WHERE id<>? AND status='pending'", a.ID)
	retry, e := s.ClaimExecution(ctx, "retry", limits)
	if e != nil || retry == nil || retry.ID != a.ID || retry.Attempt != a.Attempt+1 {
		t.Fatalf("retry %+v %v", retry, e)
	}
	if e := s.CompleteExecution(ctx, done); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("stale completion accepted", e)
	}
	old, e := s.ListAttempts(ctx, a.ID)
	if e != nil || len(old) != 2 || old[1].Status != "completed" {
		t.Fatalf("history %+v %v", old, e)
	}
	// Expiry reclaim increments attempt and fences the former worker.
	if _, e := s.db.ExecContext(ctx, "UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?", b.ID); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.ClaimExecution(ctx, "recovered", limits)
	if e != nil || recovered == nil || recovered.ID != b.ID || recovered.Attempt != b.Attempt+1 {
		t.Fatalf("recovery %+v %v", recovered, e)
	}
	if _, e = s.HeartbeatExecution(ctx, LeaseRef{TaskID: b.ID, Attempt: b.Attempt, AgentID: b.AgentID}, time.Minute, "", ""); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("stale renewal accepted", e)
	}
	if _, e = s.RequestCancel(ctx, recovered.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteExecution(ctx, Completion{LeaseRef: LeaseRef{TaskID: recovered.ID, Attempt: recovered.Attempt, AgentID: "recovered"}, Status: "completed", Findings: done.Findings}); e != nil {
		t.Fatal(e)
	}
	task, _ = s.Get(ctx, recovered.ID)
	if task.Status != "canceled" || task.FindingCount != 0 {
		t.Fatal("cancel race lost")
	}
	if _, e = s.DashboardStats(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DashboardTrend(ctx, 7); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DashboardProjectRisk(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestPostgresModelQuota(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	for i, p := range []string{"deepseek", "deepseek", "openai"} {
		_, e := s.InsertPending(ctx, Task{ScanNo: fmt.Sprint(i), SourcePath: "/src", Provider: p, Model: "m", ScanMode: "standard"})
		if e != nil {
			t.Fatal(e)
		}
	}
	limits := ClaimLimits{Global: 3, PerModel: 1, PerProject: 3, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2}
	a, e := s.ClaimExecution(ctx, "a", limits)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.ClaimExecution(ctx, "b", limits)
	if e != nil {
		t.Fatal(e)
	}
	if a == nil || b == nil || a.Provider == b.Provider {
		t.Fatal("model cap or queue fairness failed")
	}
	c, e := s.ClaimExecution(ctx, "c", limits)
	if e != nil || c != nil {
		t.Fatal("quota overflow", e)
	}
}
