package store

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func operationFixture(t *testing.T, s *Store) (Task, ClaimLimits) {
	t.Helper()
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	input, results := modernCaps()
	task, e := s.ClaimExecutionWithContracts(t.Context(), "fixture-seed", limits, input, results)
	if e != nil || task == nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), completedFixture(r, task)); e != nil {
		t.Fatal(e)
	}
	source, e := s.Get(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	findings, e := s.ListFindings(t.Context(), id)
	if e != nil || len(findings) == 0 {
		t.Fatal(e)
	}
	findingID := fmt.Sprint(findings[0].ID)
	finding, e := s.FrozenFinding(t.Context(), id, findingID)
	if e != nil {
		t.Fatal(e)
	}
	source.ID = 0
	source.InputContractVersion = 3
	source.ObservationContractVersion = 1
	source.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
	source.BudgetAccounting = true
	source.SDKOperationOrigin = id
	source.SDKOperation = &contracts.SDKOperation{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "1", Attempt: 1}, Kind: "validate", Origin: &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}, FindingID: &findingID, Finding: finding, AcknowledgeUnknownCost: true}
	source.ScanNo = "fixture-operation"
	return *source, limits
}
func TestPostgresSDKOperationCapacityExpiryAndUnknownEvidence(t *testing.T) {
	s := pgStore(t)
	task, limits := operationFixture(t, s)
	id, e := s.InsertPending(t.Context(), task)
	if e != nil {
		t.Fatal(e)
	}
	input, result := modernCaps()
	obs := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	blocked := limits
	blocked.StorageCheck = func(_ context.Context) string { return "storage_low" }
	if v, e := s.ClaimExecutionWithObservations(t.Context(), "blocked", blocked, input, result, obs); e != nil || v != nil {
		t.Fatal("storage admission bypassed", e)
	}
	settings, e := s.QueueSettings(t.Context(), limits)
	if e != nil {
		t.Fatal(e)
	}
	settings.Global = 1
	if _, e = s.SaveQueueSettings(t.Context(), settings, "fixture"); e != nil {
		t.Fatal(e)
	}
	limits.Global = 1
	first, e := s.ClaimExecutionWithObservations(t.Context(), "first", limits, input, result, obs)
	if e != nil || first == nil || first.ID != id || first.SDKOperation.Execution.Attempt != 1 {
		t.Fatal(e, first)
	}
	// Retired task-count quotas must not block independent ordinary work.
	pending, e := s.InsertPending(t.Context(), Task{ScanNo: "ordinary-competes", SourcePath: "/fixture", Provider: "openai", Model: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if next, e := s.ClaimExecution(t.Context(), "ordinary", limits); e != nil || next == nil || next.ID != pending {
		t.Fatal("retired operation capacity still blocked independent work", e, next)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, e = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at=? WHERE id=?`, past, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimExecutionWithObservations(t.Context(), "after-expiry", limits, input, result, obs); e != nil {
		t.Fatal(e)
	}
	current, e := s.Get(t.Context(), id)
	if e != nil || current.Status != "failed" || current.Attempt != 1 {
		t.Fatal("validation was replayed after lease loss", e, current)
	}
	budget, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || len(budget.UnknownAttempts) != 1 {
		t.Fatal("expired validation cost is not unknown", e)
	}
	if _, e = s.Retry(t.Context(), id); e == nil {
		t.Fatal("retry bypassed unknown-cost acknowledgement")
	}
	if e = s.DeleteScan(t.Context(), id); !errors.Is(e, ErrStorageProtected) {
		t.Fatal("unknown-cost evidence not protected", e)
	}
	if e = s.DeleteScan(t.Context(), task.SDKOperationOrigin); !errors.Is(e, ErrStorageProtected) {
		t.Fatal("operation origin evidence not protected", e)
	}
	if e = s.UpdateTotalBudget(t.Context(), id, 100, budget.Revision); !errors.Is(e, ErrBudgetPolicy) {
		t.Fatal("unsupported operation budget accepted", e)
	}
	_ = pending
}
func TestPostgresSDKOperationRejectsCrossKindIdentityAndFakeCost(t *testing.T) {
	s := pgStore(t)
	task, limits := operationFixture(t, s)
	id, e := s.InsertPending(t.Context(), task)
	if e != nil {
		t.Fatal(e)
	}
	input, results := modernCaps()
	obs := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	current, e := s.ClaimExecutionWithObservations(t.Context(), "fixture", limits, input, results, obs)
	if e != nil || current.ID != id {
		t.Fatal(e)
	}
	r := resultFixture(t)
	r.Findings = []contracts.Finding{}
	r.Execution = current.SDKOperation.Execution
	r.Cost = contracts.Some[*contracts.ScanCost](nil)
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/sdk-validation-result-reportable.json")
	if e != nil {
		t.Fatal(e)
	}
	op, e := contracts.DecodeSDKOperationResult(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	op.Execution = r.Execution
	raw, _ = json.Marshal(op)
	r.Extensions = contracts.Some(contracts.JSONObject{"sdkOperation": raw})
	completion := completedFixture(r, current)
	for _, change := range []func(*contracts.SDKOperationResult){func(v *contracts.SDKOperationResult) { v.Execution.Attempt++ }, func(v *contracts.SDKOperationResult) { zero := 0.0; v.Cost.EstimatedUSD = &zero }} {
		changed := op
		change(&changed)
		bad := completion
		result := bad.Result.Value
		data, _ := json.Marshal(changed)
		result.Extensions = contracts.Some(contracts.JSONObject{"sdkOperation": data})
		bad.Result = contracts.Some(result)
		if e = s.CompleteContractExecution(t.Context(), bad); !errors.Is(e, ErrInvalidResult) {
			t.Fatal("invalid operation result accepted", e)
		}
	}
	if e = s.CompleteContractExecution(t.Context(), completion); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), completion); e != nil {
		t.Fatal("idempotent operation completion failed", e)
	}
}

func TestPostgresSDKOperationHistoryPagesKeepOlderExecutions(t *testing.T) {
	s := pgStore(t)
	task, _ := operationFixture(t, s)
	task.SDKOperation.Kind = "preflight"
	task.SDKOperation.Finding = nil
	task.SDKOperation.FindingID = nil
	task.SDKOperation.AcknowledgeUnknownCost = false
	for i := 0; i < 53; i++ {
		task.ScanNo = fmt.Sprintf("fixture-history-%d", i)
		if _, e := s.InsertPending(t.Context(), task); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.SDKOperations(t.Context(), task.SDKOperationOrigin, 2)
	if e != nil || page.Total != 53 || page.Page != 2 || len(page.Items) != 3 {
		t.Fatal("older operation history lost", e, page)
	}
	for _, v := range page.Items {
		if v.SDKOperation == nil || v.SDKOperation.Kind != "preflight" || v.SDKOperation.Origin == nil || v.SDKOperation.Origin.TaskID != task.SDKOperation.Origin.TaskID {
			t.Fatal("operation history crossed an origin")
		}
	}
	empty, e := s.SDKOperations(t.Context(), 999999, 1)
	if e != nil || empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatal("operation history source scope lost", e)
	}
}
