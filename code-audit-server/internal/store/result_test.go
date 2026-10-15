package store

import (
	"axiom/contracts"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func resultFixture(t *testing.T) contracts.ScanResult {
	t.Helper()
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/result-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	v, e := contracts.DecodeScanResult(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func resultTask(t *testing.T, s *Store, r contracts.ScanResult) (int64, ClaimLimits) {
	t.Helper()
	snap, e := s.InsertSnapshot(t.Context(), SnapshotRow{OriginalFilename: "fixture-result.zip", ArchiveSha256: strings.Repeat("1", 64), ContentHash: strings.Repeat("2", 64), SnapshotPath: "/fixture/source"})
	if e != nil {
		t.Fatal(e)
	}
	policy := contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
	m, _ := json.Marshal(r.Model)
	p, _ := json.Marshal(policy)
	id, e := s.InsertPending(t.Context(), Task{ScanNo: fmt.Sprint("fixture-result-", snap.ID), SourcePath: snap.SnapshotPath, SourceSnapshotID: snap.ID, SourceContentHash: snap.ContentHash, Provider: string(r.Model.ProviderID), Model: r.Model.Model, BaseURL: r.Model.BaseURL.Value, TimeoutMinutes: 1, InputContractVersion: 1, ResultContractVersion: 1, ModelSnapshotJSON: string(m), PolicySnapshotJSON: string(p)})
	if e != nil {
		t.Fatal(e)
	}
	return id, ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, MaxRecoveries: 2, OutputRoot: t.TempDir()}
}

func modernCaps() (contracts.InputCapabilities, contracts.ResultCapabilities) {
	return contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}, contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
}

func completedFixture(r contracts.ScanResult, task *Task) contracts.ExecutionCompletion {
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: int64(task.Attempt)}
	code := int64(0)
	return contracts.ExecutionCompletion{SchemaVersion: 1, Execution: r.Execution, AgentID: contracts.Identifier(task.AgentID), ExecutionStatus: "completed", Process: contracts.ProcessExit{Code: &code, ResultCommitted: true}, Result: contracts.Some(r)}
}

func TestPostgresCompleteResultContract(t *testing.T) {
	s := pgStore(t)
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	input, output := modernCaps()
	if task, e := s.ClaimExecutionWithInput(t.Context(), "input-only", limits, input); e != nil || task != nil {
		t.Fatal("result capability was not enforced", task, e)
	}
	if e := s.ReportAgentWithInput(t.Context(), "input-only", 1, input); e != nil {
		t.Fatal(e)
	}
	queue, e := s.QueueOverview(t.Context(), limits, id)
	if e != nil {
		t.Fatal(e)
	}
	if queue.Tasks[0].Reason != "result_incompatible" {
		t.Fatal(queue.Tasks)
	}
	task, e := s.ClaimExecutionWithContracts(t.Context(), "fixture-agent", limits, input, output)
	if e != nil || task == nil {
		t.Fatal(e)
	}
	done := completedFixture(r, task)
	bad := done
	changed := done.Result.Value
	changed.Model.Model = "different-fixture-model"
	bad.Result = contracts.Some(changed)
	if e = s.CompleteContractExecution(t.Context(), bad); !errors.Is(e, ErrInvalidResult) {
		t.Fatal("wrong frozen model accepted", e)
	}
	bad = done
	bad.Execution.Attempt++
	bad.Result.Value.Execution = bad.Execution
	if e = s.CompleteContractExecution(t.Context(), bad); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("wrong attempt accepted", e)
	}
	// A duplicate fingerprint fails the index transaction; no full archive may
	// appear unless the matching projection commits too.
	bad = done
	bad.Result.Value.Findings = append(append([]contracts.Finding{}, r.Findings...), r.Findings[0])
	if e = s.CompleteContractExecution(t.Context(), bad); e == nil {
		t.Fatal("duplicate projection should fail")
	}
	archive, e := s.AttemptResult(t.Context(), id, task.Attempt)
	if e != nil || archive.Available {
		t.Fatal("archive survived a rolled back projection", e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal("identical retry failed", e)
	}
	archive, e = s.AttemptResult(t.Context(), id, task.Attempt)
	if e != nil {
		t.Fatal(e)
	}
	if archive.Result == nil || !reflect.DeepEqual(*archive.Result, done.Result.Value) {
		t.Fatal("full result changed during persistence")
	}
	findings, e := s.ListFindings(t.Context(), id)
	if e != nil || len(findings) != 1 || len(findings[0].Normalized) == 0 {
		t.Fatal("normalized evidence not indexed", e)
	}
	var normalized contracts.Finding
	if e = json.Unmarshal(findings[0].Normalized, &normalized); e != nil || !reflect.DeepEqual(normalized, r.Findings[0]) {
		t.Fatal("projection lost portable evidence", e)
	}
	bad = done
	bad.Result.Value.Findings = append([]contracts.Finding{}, r.Findings...)
	bad.Result.Value.Findings[0].Title = "different completion"
	if e = s.CompleteContractExecution(t.Context(), bad); !errors.Is(e, ErrCompletionConflict) {
		t.Fatal("changed completion was not a conflict", e)
	}
	if ok, e := s.Retry(t.Context(), id); e != nil || !ok {
		t.Fatal(e)
	}
	newTask, e := s.ClaimExecutionWithContracts(t.Context(), "next-agent", limits, input, output)
	if e != nil || newTask.Attempt != 2 {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("prior execution overwrote retry", e)
	}
	if _, e = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), completedFixture(r, newTask)); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("expired lease accepted", e)
	}
	old, e := s.AttemptResult(t.Context(), id, 1)
	if e != nil || old.Result == nil || !sameJSON(old.Result, done.Result.Value) {
		t.Fatal("historical full result was overwritten", e)
	}
}

func TestPostgresCancelRetainsResult(t *testing.T) {
	s := pgStore(t)
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	input, output := modernCaps()
	task, e := s.ClaimExecutionWithContracts(t.Context(), "fixture-agent", limits, input, output)
	if e != nil || task == nil {
		t.Fatal(e)
	}
	done := completedFixture(r, task)
	if _, e = s.RequestCancel(t.Context(), id); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal("retry after server cancellation failed", e)
	}
	archive, e := s.AttemptResult(t.Context(), id, task.Attempt)
	if e != nil {
		t.Fatal(e)
	}
	if archive.Completion.ExecutionStatus != "canceled" || archive.Result.Status != "completed" || len(archive.Result.Findings) != 1 {
		t.Fatal("cancellation rewrote or lost the original result")
	}
	if archive.Completion.Error.Value.Origin != "server" {
		t.Fatal("cancellation authority was not recorded")
	}
}
