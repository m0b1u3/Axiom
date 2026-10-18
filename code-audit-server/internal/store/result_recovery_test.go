package store

import (
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func recoveryFixture(t *testing.T, s *Store, total float64) (*Task, ClaimLimits, contracts.ScanResult, contracts.ResultCommit) {
	t.Helper()
	id, limits := budgetTask(t, s, total, 4)
	task := budgetClaim(t, s, limits)
	costHeartbeat(t, s, task, 1, 0)
	r := resultFixture(t)
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(task.Attempt)}
	usd := 1.25
	r.Cost = contracts.Some(&contracts.ScanCost{EstimatedUSD: &usd, Currency: "USD", IsEstimate: true})
	evidence := []byte("Fixtures / Mock: immutable recovery evidence\n")
	h := sha256.Sum256(evidence)
	r.Artifacts = []contracts.ArtifactRef{{Kind: "evidence", RelativePath: "artifacts/recovery.txt", SizeBytes: contracts.Counter(len(evidence)), SHA256: contracts.Digest(hex.EncodeToString(h[:]))}}
	for name, data := range map[string][]byte{"artifacts/recovery.txt": evidence} {
		file := filepath.Join(task.OutputPath, name)
		if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(file, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	marker := writeRecoveryFixture(t, task.OutputPath, r)
	return task, limits, r, marker
}

func writeRecoveryFixture(t *testing.T, output string, r contracts.ScanResult) contracts.ResultCommit {
	t.Helper()
	raw, e := contracts.EncodeJSON(r)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	marker := contracts.ResultCommit{SchemaVersion: 1, Execution: r.Execution, ResultSchemaVersion: 1, RelativePath: "normalized/result.json", SizeBytes: int64(len(raw)), SHA256: contracts.Digest(hex.EncodeToString(h[:]))}
	markerRaw, e := contracts.EncodeJSON(marker)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(output, "normalized"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(output, "normalized/result.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(output, "normalized/result.commit.json"), markerRaw, 0600); e != nil {
		t.Fatal(e)
	}
	return marker
}

func expireRecovery(t *testing.T, s *Store, task *Task) {
	t.Helper()
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
}

func restoreAction(v contracts.ScanRecoverySnapshot) contracts.ScanRecoveryAction {
	return contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: v.Execution, ExpectedStatus: contracts.ScanRecoveryActionExpectedStatus(v.TaskStatus), Action: "restore_result", ResultSHA256: v.CommittedResult.SHA256}
}

func TestPostgresRestoreCommittedResultWithoutNewAttempt(t *testing.T) {
	s := pgStore(t)
	task, limits, want, _ := recoveryFixture(t, s, 5)
	v, e := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e != nil || v.CommittedResult.State != "blocked" {
		t.Fatal(v, e)
	}
	digest := contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if e = s.RestoreCommittedResult(t.Context(), task.ID, contracts.ScanRecoveryAction{Execution: v.Execution, Action: "restore_result", ExpectedStatus: "scanning", ResultSHA256: &digest}, limits.OutputRoot, "fixture-operator"); !errors.Is(e, ErrRecoveryConflict) {
		t.Fatal("live lease accepted", e)
	}
	expireRecovery(t, s, task)
	v, e = s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e != nil || v.CommittedResult.State != "recoverable" || v.CheckpointResume.Supported {
		t.Fatal(v, e)
	}
	action := restoreAction(v)
	if e = s.RestoreCommittedResult(t.Context(), task.ID, action, limits.OutputRoot, "fixture-operator"); e != nil {
		t.Fatal(e)
	}
	before, e := s.BudgetSnapshot(t.Context(), task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RestoreCommittedResult(t.Context(), task.ID, action, limits.OutputRoot, "fixture-operator"); e != nil {
		t.Fatal("lost recovery ACK could not replay", e)
	}
	after, e := s.BudgetSnapshot(t.Context(), task.ID)
	if e != nil || !reflect.DeepEqual(before, after) || len(after.Attempts) != 1 || *after.EstimatedMicros != 1250000 {
		t.Fatal("recovery double billed", after, e)
	}
	history, e := s.ListAttempts(t.Context(), task.ID)
	if e != nil || len(history) != 1 || history[0].ResultRecovery == nil || history[0].ResultRecovery.RecoveredBy != "fixture-operator" {
		t.Fatal(history, e)
	}
	archive, e := s.AttemptResult(t.Context(), task.ID, 1)
	if e != nil || archive.Result == nil || !reflect.DeepEqual(*archive.Result, want) || archive.Completion.Process.Code != nil {
		t.Fatal("result or process evidence changed", archive, e)
	}
	current, _ := s.Get(t.Context(), task.ID)
	if current.Attempt != 1 || current.Status != "completed" {
		t.Fatal(current)
	}
	v, e = s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e != nil || v.CommittedResult.State != "archived" || v.Recovery == nil {
		t.Fatal(v, e)
	}
	if _, e = s.HeartbeatExecution(t.Context(), LeaseRef{TaskID: task.ID, Attempt: 1, AgentID: task.AgentID}, limits.Lease, "", ""); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("old worker retained lease", e)
	}
}

func TestPostgresRecoveryRejectsInvalidEvidence(t *testing.T) {
	for _, scenario := range []string{"no-marker", "truncated-result", "digest", "wrong-execution", "wrong-model", "missing-artifact", "changed-artifact", "artifact-symlink", "directory-symlink", "outside-root"} {
		t.Run(scenario, func(t *testing.T) {
			s := pgStore(t)
			task, limits, r, marker := recoveryFixture(t, s, 5)
			expireRecovery(t, s, task)
			root := limits.OutputRoot
			switch scenario {
			case "no-marker":
				os.Remove(filepath.Join(task.OutputPath, "normalized/result.commit.json"))
			case "truncated-result":
				os.WriteFile(filepath.Join(task.OutputPath, "normalized/result.json"), []byte("{\"schemaVersion\":"), 0600)
			case "digest":
				marker.SHA256 = contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				b, _ := contracts.EncodeJSON(marker)
				os.WriteFile(filepath.Join(task.OutputPath, "normalized/result.commit.json"), b, 0600)
			case "wrong-execution":
				r.Execution.Attempt++
				marker = writeRecoveryFixture(t, task.OutputPath, r)
			case "wrong-model":
				r.Model.Model = "fixture-unrelated-model"
				marker = writeRecoveryFixture(t, task.OutputPath, r)
			case "missing-artifact":
				os.Remove(filepath.Join(task.OutputPath, "artifacts/recovery.txt"))
			case "changed-artifact":
				os.WriteFile(filepath.Join(task.OutputPath, "artifacts/recovery.txt"), []byte("altered"), 0600)
			case "artifact-symlink":
				file := filepath.Join(task.OutputPath, "artifacts/recovery.txt")
				data, _ := os.ReadFile(file)
				target := filepath.Join(t.TempDir(), "outside.txt")
				os.WriteFile(target, data, 0600)
				os.Remove(file)
				if e := os.Symlink(target, file); e != nil {
					t.Fatal(e)
				}
			case "directory-symlink":
				os.Rename(filepath.Join(task.OutputPath, "normalized"), filepath.Join(task.OutputPath, "replaced-normalized"))
				if e := os.Symlink(filepath.Join(task.OutputPath, "replaced-normalized"), filepath.Join(task.OutputPath, "normalized")); e != nil {
					t.Fatal(e)
				}
			case "outside-root":
				root = t.TempDir()
			}
			action := contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: contracts.RecoveryExecution{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}, ExpectedStatus: "scanning", Action: "restore_result", ResultSHA256: &marker.SHA256}
			if e := s.RestoreCommittedResult(t.Context(), task.ID, action, root, "fixture-operator"); e == nil {
				t.Fatal("invalid recovery accepted")
			}
			current, _ := s.Get(t.Context(), task.ID)
			if current.Status != "scanning" || current.Attempt != 1 || current.FindingCount != 0 {
				t.Fatal("rejected recovery mutated task", current)
			}
			if record, e := s.ResultRecovery(t.Context(), task.ID, 1); e != nil || record != nil {
				t.Fatal("rejected recovery recorded success", record, e)
			}
		})
	}
}

func TestPostgresRecoveryInterruptedFeeAndManualEstimate(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprint(boolInt(manual)), func(t *testing.T) {
			s := pgStore(t)
			task, limits, _, _ := recoveryFixture(t, s, 5)
			costHeartbeat(t, s, task, 2, 0.5)
			expireRecovery(t, s, task)
			v, _ := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
			action := contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: v.Execution, ExpectedStatus: "scanning", Action: "interrupt_expired"}
			if e := s.MarkExecutionInterrupted(t.Context(), task.ID, action); e != nil {
				t.Fatal(e)
			}
			b, e := s.BudgetSnapshot(t.Context(), task.ID)
			if e != nil || b.RetryAllowed || len(b.UnknownAttempts) != 1 {
				t.Fatal("interrupted fee became zero", b, e)
			}
			if _, e = s.RetryExecution(t.Context(), task.ID, 1, "failed"); !errors.Is(e, ErrBudgetUnknown) {
				t.Fatal(e)
			}
			if manual {
				if e = s.ReconcileAttemptCost(t.Context(), task.ID, 1, 2, "Fixture manual fee evidence", "fixture-operator", b.Revision); e != nil {
					t.Fatal(e)
				}
			}
			v, _ = s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
			if e = s.RestoreCommittedResult(t.Context(), task.ID, restoreAction(v), limits.OutputRoot, "fixture-operator"); e != nil {
				t.Fatal(e)
			}
			b, e = s.BudgetSnapshot(t.Context(), task.ID)
			if e != nil || len(b.UnknownAttempts) != 0 {
				t.Fatal(b, e)
			}
			if manual && (*b.EstimatedMicros != 2000000 || b.Attempts[0].Source != "manual_estimate" || b.Attempts[0].Note != "Fixture manual fee evidence") {
				t.Fatal("manual estimate or provenance changed", b)
			}
			if !manual && *b.EstimatedMicros != 1250000 {
				t.Fatal(b)
			}
		})
	}
}

func TestPostgresRecoveryKeepsUnknownAndCancellation(t *testing.T) {
	for _, state := range []string{"canceled", "timeout", "cancel_requested", "unknown-cost"} {
		t.Run(state, func(t *testing.T) {
			s := pgStore(t)
			task, limits, r, _ := recoveryFixture(t, s, 5)
			expireRecovery(t, s, task)
			if state == "unknown-cost" {
				r.Cost = contracts.Some[*contracts.ScanCost](nil)
				writeRecoveryFixture(t, task.OutputPath, r)
			} else {
				if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET status=? WHERE id=?`, state, task.ID); e != nil {
					t.Fatal(e)
				}
			}
			v, e := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.RestoreCommittedResult(t.Context(), task.ID, restoreAction(v), limits.OutputRoot, "fixture-operator"); e != nil {
				t.Fatal(e)
			}
			current, _ := s.Get(t.Context(), task.ID)
			want := state
			if state == "cancel_requested" {
				want = "canceled"
			}
			if state == "unknown-cost" {
				want = "completed"
			}
			if current.Status != want {
				t.Fatal("stop semantics changed", current)
			}
			if state == "unknown-cost" {
				b, _ := s.BudgetSnapshot(t.Context(), task.ID)
				if len(b.UnknownAttempts) != 1 || b.RetryAllowed {
					t.Fatal("recovery inferred known zero", b)
				}
			}
		})
	}
}

func TestPostgresRecoveryConcurrentReplayAndRestartFence(t *testing.T) {
	s := pgStore(t)
	task, limits, _, _ := recoveryFixture(t, s, 5)
	expireRecovery(t, s, task)
	v, _ := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	action := restoreAction(v)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.RestoreCommittedResult(t.Context(), task.ID, action, limits.OutputRoot, "fixture-operator")
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("idempotent recovery replay failed", e)
		}
	}
	if _, e := s.RetryExecution(t.Context(), task.ID, 1, "scanning"); !errors.Is(e, ErrRecoveryConflict) {
		t.Fatal("stale restart confirmation accepted", e)
	}
	if ok, e := s.RetryExecution(t.Context(), task.ID, 1, "completed"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if e := s.RestoreCommittedResult(t.Context(), task.ID, action, limits.OutputRoot, "fixture-operator"); !errors.Is(e, ErrRecoveryConflict) {
		t.Fatal("recovery after requeue accepted", e)
	}
	next := budgetClaim(t, s, limits)
	if next.Attempt != 2 || *next.ExecutionMaxCostUSD != 3.75 {
		t.Fatal("retry grant did not use remaining total", next)
	}
	if e := s.RestoreCommittedResult(t.Context(), task.ID, action, limits.OutputRoot, "fixture-operator"); !errors.Is(e, ErrRecoveryConflict) {
		t.Fatal("old attempt overwrote new execution", e)
	}
	history, _ := s.ListAttempts(t.Context(), task.ID)
	if len(history) != 2 || history[1].ResultRecovery == nil {
		t.Fatal("history lost recovery evidence", history)
	}
}

func TestPostgresRecoveryRetainsPreviousCompletionAndRollsBackAtomically(t *testing.T) {
	for _, invalidProjection := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidProjection), func(t *testing.T) {
			s := pgStore(t)
			task, limits, r, _ := recoveryFixture(t, s, 5)
			original := completedFixture(r, task)
			original.Result = contracts.Optional[contracts.ScanResult]{}
			original.Process.ResultCommitted = false
			original.ExecutionStatus = "failed"
			original.Error = contracts.Some(contracts.ContractError{Code: "invalid_result", Origin: "agent", Message: "Fixture: commit temporarily unreadable", Retryable: false})
			if e := s.CompleteContractExecution(t.Context(), original); e != nil {
				t.Fatal(e)
			}
			if invalidProjection {
				r.Findings = append(r.Findings, r.Findings[0])
				writeRecoveryFixture(t, task.OutputPath, r)
			}
			v, e := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
			if e != nil {
				t.Fatal(e)
			}
			e = s.RestoreCommittedResult(t.Context(), task.ID, restoreAction(v), limits.OutputRoot, "fixture-operator")
			if invalidProjection {
				if e == nil {
					t.Fatal("duplicate finding projection was accepted")
				}
				archive, _ := s.AttemptResult(t.Context(), task.ID, 1)
				if archive.Result != nil || !reflect.DeepEqual(*archive.Completion, original) {
					t.Fatal("failed transaction replaced original archive")
				}
				if record, _ := s.ResultRecovery(t.Context(), task.ID, 1); record != nil {
					t.Fatal("rolled-back recovery left success record")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				record, e := s.ResultRecovery(t.Context(), task.ID, 1)
				if e != nil || record.PreviousErrorMessage != original.Error.Value.Message || record.PreviousCompletion == nil || !reflect.DeepEqual(*record.PreviousCompletion, original) {
					t.Fatal("prior error/process evidence not retained", record, e)
				}
			}
		})
	}
}

func TestPostgresRecoveryKeepsManualProvenanceWhenFinalRaisesEstimate(t *testing.T) {
	s := pgStore(t)
	task, limits, _, _ := recoveryFixture(t, s, 5)
	expireRecovery(t, s, task)
	action := contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: contracts.RecoveryExecution{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}, ExpectedStatus: "scanning", Action: "interrupt_expired"}
	if e := s.MarkExecutionInterrupted(t.Context(), task.ID, action); e != nil {
		t.Fatal(e)
	}
	b, _ := s.BudgetSnapshot(t.Context(), task.ID)
	if e := s.ReconcileAttemptCost(t.Context(), task.ID, 1, 0.75, "Fixture conservative manual estimate", "fixture-operator", b.Revision); e != nil {
		t.Fatal(e)
	}
	v, _ := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e := s.RestoreCommittedResult(t.Context(), task.ID, restoreAction(v), limits.OutputRoot, "fixture-operator"); e != nil {
		t.Fatal(e)
	}
	b, e := s.BudgetSnapshot(t.Context(), task.ID)
	if e != nil || *b.EstimatedMicros != 1250000 || b.Attempts[0].Source != "manual_estimate" || b.Attempts[0].Reason != "recovered_final_above_manual" || b.Attempts[0].Note != "Fixture conservative manual estimate" {
		t.Fatal(b, e)
	}
	record, e := s.ResultRecovery(t.Context(), task.ID, 1)
	if e != nil || record.PreviousCost == nil || *record.PreviousCost.BookedMicros != 750000 || record.PreviousCost.Source != "manual_estimate" || record.PreviousCost.Note != "Fixture conservative manual estimate" {
		t.Fatal("original manual amount or evidence was lost", record, e)
	}
}

func TestPostgresRecoveryMigrationKeepsExistingArchiveAndLedger(t *testing.T) {
	s := pgStore(t)
	task, limits, _, _ := recoveryFixture(t, s, 5)
	expireRecovery(t, s, task)
	v, _ := s.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e := s.RestoreCommittedResult(t.Context(), task.ID, restoreAction(v), limits.OutputRoot, "fixture-operator"); e != nil {
		t.Fatal(e)
	}
	archive, e := s.AttemptResult(t.Context(), task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	budget, e := s.BudgetSnapshot(t.Context(), task.ID)
	if e != nil {
		t.Fatal(e)
	}
	// Simulate a pre-017 database. Its historical result and ledger must be
	// byte/semantically preserved when the append-only migration is applied.
	if _, e = s.db.ExecContext(t.Context(), `DROP TABLE scan_result_recoveries; DELETE FROM schema_migrations WHERE version='017_result_recovery.sql'`); e != nil {
		t.Fatal(e)
	}
	upgraded, e := Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	after, e := upgraded.AttemptResult(t.Context(), task.ID, 1)
	if e != nil || !reflect.DeepEqual(after, archive) {
		t.Fatal("migration changed historical archive", e)
	}
	b, e := upgraded.BudgetSnapshot(t.Context(), task.ID)
	if e != nil || !reflect.DeepEqual(b, budget) {
		t.Fatal("migration changed ledger", e)
	}
	state, e := upgraded.RecoverySnapshot(t.Context(), task.ID, limits.OutputRoot)
	if e != nil || state.CommittedResult.State != "archived" || state.Recovery != nil {
		t.Fatal(state, e)
	}
}
