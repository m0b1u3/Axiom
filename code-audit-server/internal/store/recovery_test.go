package store

import (
	"fmt"
	"testing"
	"time"
)

func TestPostgresRecoveryStopsAtConfiguredLimit(t *testing.T) {
	for _, max := range []int{0, 2} {
		t.Run(fmt.Sprint(max), func(t *testing.T) {
			s := pgStore(t)
			ctx := t.Context()
			id, e := s.InsertPending(ctx, Task{ScanNo: "fixture-recovery-limit", SourcePath: "/src", Provider: "openai", Model: "fixture", ScanMode: "standard"})
			if e != nil {
				t.Fatal(e)
			}
			limits := ClaimLimits{Global: 1, PerModel: 1, PerProject: 1, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: max}
			for attempt := 1; attempt <= max+1; attempt++ {
				claim, e := s.ClaimExecution(ctx, fmt.Sprintf("agent-%d", attempt), limits)
				if e != nil || claim == nil || claim.ID != id || claim.Attempt != attempt {
					t.Fatal("recovery attempt", claim, e)
				}
				if _, e := s.db.ExecContext(ctx, "UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?", id); e != nil {
					t.Fatal(e)
				}
			}
			for i := 0; i < 2; i++ {
				claim, e := s.ClaimExecution(ctx, "extra-agent", limits)
				if e != nil || claim != nil {
					t.Fatal("recovery exceeded the cap", claim, e)
				}
			}
			task, e := s.Get(ctx, id)
			if e != nil || task.Status != "failed" || task.ErrorCode != "lease_expired" || task.Attempt != max+1 {
				t.Fatal(task, e)
			}
			history, e := s.ListAttempts(ctx, id)
			if e != nil || len(history) != max+1 || history[0].Status != "failed" {
				t.Fatal(history, e)
			}
			for _, previous := range history[1:] {
				if previous.Status != "abandoned" {
					t.Fatal("lost recovery history", previous)
				}
			}
		})
	}
}
