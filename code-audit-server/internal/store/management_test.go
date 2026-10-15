package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPostgresManagement(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	limits := ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2}
	cfg, e := s.QueueSettings(ctx, limits)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Global = 1
	updated, e := s.SaveQueueSettings(ctx, cfg, "admin")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveQueueSettings(ctx, cfg, "admin"); !errors.Is(e, ErrSettingsConflict) {
		t.Fatal("stale settings accepted", e)
	}
	if e = s.ReportAgent(ctx, "qa-agent", 2); e != nil {
		t.Fatal(e)
	}
	a, e := s.InsertPending(ctx, Task{ScanNo: "a", SourcePath: "/src", Provider: "test", ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.InsertPending(ctx, Task{ScanNo: "b", SourcePath: "/src", Provider: "test", ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	claimed, e := s.ClaimExecution(ctx, "qa-agent", limits)
	if e != nil || claimed == nil || claimed.ID != a {
		t.Fatal("first claim", e)
	}
	v, e := s.QueueOverview(ctx, limits, 0)
	if e != nil {
		t.Fatal(e)
	}
	if v.Active != 1 || v.Pending != 1 || v.EffectiveCapacity != 1 || v.Tasks[1].Reason != "global_limit" {
		t.Fatalf("overview %+v", v)
	}
	blocked, e := s.ClaimExecution(ctx, "qa-agent", limits)
	if e != nil || blocked != nil {
		t.Fatal("dynamic cap not enforced", e)
	}
	done := Completion{LeaseRef: LeaseRef{TaskID: a, Attempt: 1, AgentID: "qa-agent"}, Status: "completed", Findings: []Finding{{Fingerprint: "a", Title: "evidence", Severity: "high", Description: "test"}}}
	if e = s.CompleteExecution(ctx, done); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Retry(ctx, a); e != nil {
		t.Fatal(e)
	}
	result, e := s.AttemptResult(ctx, a, 1)
	if e != nil || !result.Available || len(result.Findings) != 1 {
		t.Fatal("history lost", e)
	}
	claimed, e = s.ClaimExecution(ctx, "qa-agent", limits)
	if e != nil || claimed == nil || claimed.ID != b {
		t.Fatal("manual retry jumped queue", e)
	}
	updated.Global = 2
	if _, e = s.SaveQueueSettings(ctx, updated, "admin"); e != nil {
		t.Fatal(e)
	}
	next, e := s.ClaimExecution(ctx, "qa-agent", limits)
	if e != nil || next == nil || next.ID != a {
		t.Fatal("settings not effective", e)
	}
	if _, e = s.RequestCancel(ctx, b); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, b); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimExecution(ctx, "qa-agent", limits); e != nil {
		t.Fatal(e)
	}
	canceled, e := s.Get(ctx, b)
	if e != nil || canceled.Status != "canceled" || canceled.Attempt != 1 {
		t.Fatal("cancel expiry restarted", e)
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE execution_agents SET last_seen=NOW()-INTERVAL '1 minute'`); e != nil {
		t.Fatal(e)
	}
	v, e = s.QueueOverview(ctx, limits, a)
	if e != nil || v.OnlineSlots != 0 {
		t.Fatal("offline agent not recognized", e)
	}
	if e = s.CompleteExecution(ctx, Completion{LeaseRef: LeaseRef{TaskID: a, Attempt: 2, AgentID: "qa-agent"}, Status: "failed", ErrorCode: "scanner_error"}); e != nil {
		t.Fatal(e)
	}
	if attempts, e := s.ListAttempts(ctx, a); e != nil || len(attempts) != 2 || attempts[0].FindingCount != 0 {
		t.Fatal("nil findings history", e)
	}
	history, e := s.QueueSettingHistory(ctx)
	if e != nil || len(history) != 2 {
		t.Fatal("config history", e)
	}
}
