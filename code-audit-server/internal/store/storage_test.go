package store

import (
	"axiom/contracts"
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestStorageSQLiteImportKeepsPolicyAndInterruptedIntent(t *testing.T) {
	target := pgStore(t)
	file := filepath.Join(t.TempDir(), "source.db")
	source, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	p := contracts.StoragePolicy{Revision: 1, MinFreeBytes: 0, OrphanGraceHours: 72}
	if e = source.UpdateStoragePolicy(t.Context(), p); e != nil {
		t.Fatal(e)
	}
	expected, e := source.StoragePolicy(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	stamp := contracts.Timestamp("2026-10-15T00:00:00Z")
	job := contracts.CleanupJob{SchemaVersion: 1, ID: "fixture-imported-job", Kind: "orphans", Revision: 3, State: "failed", CreatedAt: stamp, UpdatedAt: stamp, ExpiresAt: stamp, CreatedBy: "fixture-admin", Error: "Fixture interrupted cleanup", Items: []contracts.CleanupItem{}}
	if e = source.SaveCleanupJob(t.Context(), job, `{"scans":"/fixture"}`, true); e != nil {
		t.Fatal(e)
	}
	if _, e = target.ImportSQLite(t.Context(), file); e != nil {
		t.Fatal(e)
	}
	got, e := target.StoragePolicy(t.Context())
	if e != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal(got, expected, e)
	}
	restored, roots, e := target.CleanupJob(t.Context(), string(job.ID))
	if e != nil || !reflect.DeepEqual(restored, job) || roots != `{"scans":"/fixture"}` {
		t.Fatal(restored, roots, e)
	}
}

func TestStorageAdmissionStillFinalizesExpiredLease(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	first := budgetClaim(t, s, limits)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	limits.StorageCheck = func(context.Context) string { return "storage_low" }
	input, result := modernCaps()
	got, e := s.ClaimExecutionWithObservations(t.Context(), "storage-agent", limits, input, result, observationCaps())
	if e != nil || got != nil {
		t.Fatal(got, e)
	}
	task, e := s.Get(t.Context(), id)
	if e != nil || task.Status != "failed" || task.Attempt != first.Attempt {
		t.Fatal("storage admission skipped lease maintenance", task, e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.State != "unknown" {
		t.Fatal(b, e)
	}
}
func TestStorageGateWaitersDoNotExhaustTransactionPool(t *testing.T) {
	s := pgStore(t)
	s.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	out := make(chan error, 32)
	e := s.StorageLock(ctx, true, func() error {
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); out <- s.StorageLock(ctx, false, func() error { return nil }) }()
		}
		deadline := time.Now().Add(time.Second)
		for s.storageGateDB.Stats().InUse < 16 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		// If gate waiters occupy the transaction pool, this write deadlocks.
		return s.UpdateStoragePolicy(ctx, contracts.StoragePolicy{Revision: 1, MinFreeBytes: 0, OrphanGraceHours: 48})
	})
	if e != nil {
		t.Fatal(e)
	}
	wg.Wait()
	close(out)
	for e := range out {
		if e != nil {
			t.Fatal(e)
		}
	}
	p, e := s.StoragePolicy(t.Context())
	if e != nil || p.Revision != 2 || p.OrphanGraceHours != 48 {
		t.Fatal(p, e)
	}
}
