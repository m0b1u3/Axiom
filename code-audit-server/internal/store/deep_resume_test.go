package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func deepFixture(t *testing.T) (*Store, *Task, ClaimLimits) {
	t.Helper()
	s := pgStore(t)
	id, limits := resultTask(t, s, resultFixture(t))
	limits.SDKDeepResumeCapable = true
	limits.SDKConcurrencyCapable = true
	policy := contracts.ScanPolicy{Mode: "deep", TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
	p, _ := contracts.EncodeJSON(policy)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET scan_mode='deep',observation_contract_version=1,policy_snapshot_json=? WHERE id=?`, string(p), id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.ExecContext(t.Context(), `INSERT INTO task_budgets(task_id,total_micros) VALUES(?,NULL)`, id); e != nil {
		t.Fatal(e)
	}
	input, result := modernCaps()
	agent := "fixture-deep-agent"
	if e := s.ReportAgentWithObservations(t.Context(), agent, 1, input, result, observationCaps()); e != nil {
		t.Fatal(e)
	}
	deepRuntime(t, s, agent, 14)
	task, e := s.ClaimExecutionWithObservations(t.Context(), agent, limits, input, result, observationCaps())
	if e != nil || task == nil {
		t.Fatal("initial claim", task, e)
	}
	code := int64(1)
	e = s.CompleteContractExecution(t.Context(), contracts.ExecutionCompletion{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}, AgentID: contracts.Identifier(agent), Error: contracts.Some(contracts.ContractError{Code: "unknown", Origin: "agent", Message: "Fixtures / Mock abrupt interruption", Retryable: false}), ExecutionStatus: "failed", Process: contracts.ProcessExit{Code: &code, ResultCommitted: false}})
	if e != nil {
		t.Fatal(e)
	}
	task, e = s.Get(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	return s, task, limits
}
func deepRuntime(t *testing.T, s *Store, agent string, version int64) {
	t.Helper()
	build := "fixture-deep"
	if e := s.ReportAgentRuntime(t.Context(), agent, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixture", ScannerBuild: &build, ControlAPI: 4, State: "ready", MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}); e != nil {
		t.Fatal(e)
	}
}
func deepChecked(t *testing.T, s *Store, task *Task) contracts.DeepResumeSnapshot {
	t.Helper()
	ref := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: int64(task.Attempt)}
	id, e := s.RequestDeepResumeCheck(t.Context(), task.ID, ref)
	if e != nil {
		t.Fatal("request check", e)
	}
	if v, e := s.ClaimDeepResumeCheck(t.Context(), "foreign-agent"); e != nil || v != nil {
		t.Fatal("foreign probe", v, e)
	}
	claim, e := s.ClaimDeepResumeCheck(t.Context(), task.AgentID)
	if e != nil || claim == nil || claim.ID != id {
		t.Fatal("claim check", claim, e)
	}
	scan, thread, proof := "fixture-original-scan", "fixture-original-thread", strings.Repeat("c", 64)
	usd := 1.25
	inspection := contracts.DeepResumeInspection{SchemaVersion: 1, SDKVersion: "0.5.0", Execution: claim.Execution, Origin: claim.Origin, AgentID: claim.AgentID, CheckedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano)), Eligible: true, Reason: "Fixtures / Mock native conditions passed", SourceHash: claim.SourceHash, ConfigurationHash: claim.ConfigurationHash, ScanID: &scan, ThreadID: &thread, ProofHash: &proof, Baseline: contracts.DeepResumeBaseline{EstimatedUSD: &usd}}
	report := contracts.DeepResumeProbeReport{SchemaVersion: 1, ID: claim.ID, LeaseToken: claim.LeaseToken, AgentID: claim.AgentID, Inspection: inspection}
	bad := report
	bad.LeaseToken = "foreign-lease"
	if e = s.ReportDeepResumeCheck(t.Context(), bad); !errors.Is(e, ErrDeepResume) {
		t.Fatal("foreign proof accepted", e)
	}
	if e = s.ReportDeepResumeCheck(t.Context(), report); e != nil {
		t.Fatal("report", e)
	}
	v, e := s.DeepResumeSnapshot(t.Context(), task.ID)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func deepAction(v contracts.DeepResumeSnapshot) contracts.DeepResumeAction {
	return contracts.DeepResumeAction{SchemaVersion: 1, Execution: v.Execution, ProbeID: *v.ProbeID, ProofHash: *v.Inspection.ProofHash, AcknowledgeAdditionalCost: true}
}
func TestDeepResumeAdmissionAndAgentFencing(t *testing.T) {
	s, task, limits := deepFixture(t)
	v := deepChecked(t, s, task)
	if !v.CanResume || v.State != "ready" {
		t.Fatal(v)
	}
	action := deepAction(v)
	bad := action
	bad.AcknowledgeAdditionalCost = false
	if e := s.ResumeDeepScan(t.Context(), task.ID, bad); !errors.Is(e, ErrDeepResume) {
		t.Fatal("ack bypass", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.ResumeDeepScan(t.Context(), task.ID, action) }()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrDeepResume) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("duplicate admission", success)
	}
	pending, e := s.DeepResumeSnapshot(t.Context(), task.ID)
	if e != nil || pending.State != "pending" || pending.CanResume || pending.Continuation == nil {
		t.Fatal("pending snapshot", pending, e)
	}
	input, result := modernCaps()
	for _, agent := range []string{"foreign-agent", task.AgentID} {
		l := limits
		if agent == task.AgentID {
			l.SDKDeepResumeCapable = false
		}
		claim, e := s.ClaimExecutionWithObservations(t.Context(), agent, l, input, result, observationCaps())
		if e != nil || claim != nil {
			t.Fatal("unqualified resume claimed", claim, e)
		}
	}
	deepRuntime(t, s, task.AgentID, 13)
	claim, e := s.ClaimExecutionWithObservations(t.Context(), task.AgentID, limits, input, result, observationCaps())
	if e != nil || claim != nil {
		t.Fatal("old scanner claimed continuation", claim, e)
	}
	deepRuntime(t, s, task.AgentID, 14)
	claim, e = s.ClaimExecutionWithObservations(t.Context(), task.AgentID, limits, input, result, observationCaps())
	if e != nil || claim == nil || claim.Attempt != 2 || claim.DeepResume == nil || claim.DeepResume.Inspection.ScanID == nil || *claim.DeepResume.Inspection.ScanID != "fixture-original-scan" || claim.DeepResume.Previous.Attempt != 1 {
		t.Fatal("continuation identity lost", claim, e)
	}
	if _, e = s.HeartbeatExecution(t.Context(), LeaseRef{TaskID: task.ID, Attempt: 1, AgentID: task.AgentID}, time.Minute, "", ""); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("old execution updated continuation", e)
	}
	if _, e = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
	again, e := s.ClaimExecutionWithObservations(t.Context(), task.AgentID, limits, input, result, observationCaps())
	if e != nil || again != nil {
		t.Fatal("continuation was silently restarted", again, e)
	}
	history, e := s.ListAttempts(t.Context(), task.ID)
	if e != nil || len(history) != 2 {
		t.Fatal(history, e)
	}
}
func TestDeepResumeExpiryIdentityAndBudgetFailures(t *testing.T) {
	for _, scenario := range []string{"stale-proof", "changed-source", "changed-config", "unknown-cost", "exhausted-budget", "native-unknown", "revoked-identity", "cancel-pending"} {
		t.Run(scenario, func(t *testing.T) {
			s, task, _ := deepFixture(t)
			if scenario == "revoked-identity" {
				identity, e := s.CreateSDKIdentity(t.Context(), "Fixtures / Mock", task.AgentID, "api_key")
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal(identity.Identity)
				if _, e = s.db.ExecContext(t.Context(), `UPDATE sdk_identities SET state='authenticated',authenticated=TRUE WHERE id=?`, identity.Identity.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = s.db.ExecContext(t.Context(), `INSERT INTO task_sdk_identities(task_id,identity_id,reference_json) VALUES(?,?,?)`, task.ID, identity.Identity.ID, string(raw)); e != nil {
					t.Fatal(e)
				}
				task, _ = s.Get(t.Context(), task.ID)
			}
			v := deepChecked(t, s, task)
			action := deepAction(v)
			switch scenario {
			case "stale-proof":
				_, _ = s.db.ExecContext(t.Context(), `UPDATE deep_resume_checks SET updated_at='2000-01-01T00:00:00Z' WHERE id=?`, action.ProbeID)
			case "changed-source":
				_, _ = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET source_content_hash=? WHERE id=?`, strings.Repeat("d", 64), task.ID)
			case "changed-config":
				_, _ = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET model_snapshot_json='{}' WHERE id=?`, task.ID)
			case "unknown-cost", "exhausted-budget":
				_, _ = s.db.ExecContext(t.Context(), `UPDATE task_budgets SET total_micros=? WHERE task_id=?`, 1000000, task.ID)
				state := "unknown"
				if scenario == "exhausted-budget" {
					state = "complete"
				}
				_, _ = s.db.ExecContext(t.Context(), `UPDATE task_cost_attempts SET state=?,booked_micros=1000000 WHERE task_id=?`, state, task.ID)
			case "native-unknown":
				p := *v.Inspection
				p.Baseline.EstimatedUSD = nil
				raw, _ := json.Marshal(p)
				_, _ = s.db.ExecContext(t.Context(), `UPDATE deep_resume_checks SET inspection_json=? WHERE id=?`, string(raw), action.ProbeID)
				_, _ = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET max_cost_usd=3 WHERE id=?`, task.ID)
			case "revoked-identity":
				_, _ = s.db.ExecContext(t.Context(), `UPDATE sdk_identities SET generation=generation+1,state='revoked',authenticated=FALSE`)
			case "cancel-pending":
				if e := s.ResumeDeepScan(t.Context(), task.ID, action); e != nil {
					t.Fatal(e)
				}
				if ok, e := s.RequestCancel(t.Context(), task.ID); e != nil || !ok {
					t.Fatal(ok, e)
				}
			}
			if e := s.ResumeDeepScan(t.Context(), task.ID, action); e == nil {
				t.Fatal("unsafe continuation accepted", scenario)
			}
		})
	}
}
func TestDeepResumeRemainingBudgetAndProbeFailure(t *testing.T) {
	s, task, limits := deepFixture(t)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET max_cost_usd=2 WHERE id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.ExecContext(t.Context(), `UPDATE task_budgets SET total_micros=2500000 WHERE task_id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.ExecContext(t.Context(), `UPDATE task_cost_attempts SET state='complete',booked_micros=1000000,final_micros=1000000 WHERE task_id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
	task, _ = s.Get(t.Context(), task.ID)
	v := deepChecked(t, s, task)
	if !v.CanResume {
		t.Fatal(v)
	}
	if e := s.ResumeDeepScan(t.Context(), task.ID, deepAction(v)); e != nil {
		t.Fatal(e)
	}
	input, result := modernCaps()
	next, e := s.ClaimExecutionWithObservations(t.Context(), task.AgentID, limits, input, result, observationCaps())
	if e != nil || next == nil || next.ExecutionMaxCostUSD == nil || *next.ExecutionMaxCostUSD != 1.5 || *next.MaxCostUSD != 2 {
		t.Fatal("remaining or single cap changed", next, e)
	}
}

func TestDeepResumeResultLineageAndIncrementalCostBinding(t *testing.T) {
	s, task, limits := deepFixture(t)
	view := deepChecked(t, s, task)
	if e := s.ResumeDeepScan(t.Context(), task.ID, deepAction(view)); e != nil {
		t.Fatal(e)
	}
	input, result := modernCaps()
	next, e := s.ClaimExecutionWithObservations(t.Context(), task.AgentID, limits, input, result, observationCaps())
	if e != nil || next == nil {
		t.Fatal(next, e)
	}
	r := resultFixture(t)
	sdkVersion := "0.5.0"
	r.Engine.Version = &sdkVersion
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 2}
	lineage, _ := json.Marshal(next.DeepResume)
	native, _ := json.Marshal(map[string]any{"scanId": *view.Inspection.ScanID, "cost": map[string]any{"estimatedUsd": 2.0}})
	r.Extensions = contracts.Some(contracts.JSONObject{"deepResume": lineage, "sdk": native})
	usd := .75
	r.Cost = contracts.Some(&contracts.ScanCost{EstimatedUSD: &usd, Currency: "USD", IsEstimate: true})
	full := completedFixture(r, next)
	if e = s.validateDeepResumeResult(t.Context(), task.ID, full); e != nil {
		t.Fatal("valid lineage rejected", e)
	}
	for _, scenario := range []string{"missing-lineage", "different-lineage", "different-scan", "cumulative-double-charge", "unbounded-price-settled"} {
		t.Run(scenario, func(t *testing.T) {
			raw, _ := json.Marshal(full)
			var bad contracts.ExecutionCompletion
			if e = json.Unmarshal(raw, &bad); e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "missing-lineage":
				delete(bad.Result.Value.Extensions.Value, "deepResume")
			case "different-lineage":
				wrong := *next.DeepResume
				wrong.Previous.Attempt = 99
				b, _ := json.Marshal(wrong)
				bad.Result.Value.Extensions.Value["deepResume"] = b
			case "different-scan":
				bad.Result.Value.Extensions.Value["sdk"] = json.RawMessage(`{"scanId":"foreign","cost":{"estimatedUsd":2}}`)
			case "cumulative-double-charge":
				v := 2.0
				bad.Result.Value.Cost.Value.EstimatedUSD = &v
			case "unbounded-price-settled":
				bad.Result.Value.Extensions.Value["sdk"] = json.RawMessage(fmt.Sprintf(`{"scanId":%q,"cost":{"estimatedUsd":2,"estimatedUsdRange":{"min":2,"max":null}}}`, *view.Inspection.ScanID))
			}
			if e = s.validateDeepResumeResult(t.Context(), task.ID, bad); !errors.Is(e, ErrInvalidResult) {
				t.Fatal("foreign lineage or cumulative cost accepted", e)
			}
		})
	}
	if e = s.CompleteContractExecution(t.Context(), full); e != nil {
		t.Fatal(e)
	}
}
func TestDeepResumeProtectsCheckpointFromCleanupAndLostProbeLease(t *testing.T) {
	s, task, _ := deepFixture(t)
	view := deepChecked(t, s, task)
	if _, e := s.CleanupResourcePaths(t.Context(), "scan", task.ID, "fixture-cleanup"); !errors.Is(e, ErrStorageProtected) {
		t.Fatal("native proof was cleaned", e)
	}
	if _, e := s.db.ExecContext(t.Context(), `UPDATE deep_resume_checks SET state='checking',lease_until='2000-01-01T00:00:00Z' WHERE id=?`, *view.ProbeID); e != nil {
		t.Fatal(e)
	}
	stale, e := s.DeepResumeSnapshot(t.Context(), task.ID)
	if e != nil || stale.CanResume || stale.State != "blocked" || !stale.CanCheck {
		t.Fatal("lost inspection lease trapped Web", stale, e)
	}
}

func TestDeepResumeExpiredProofDoesNotPermanentlyBlockExplicitCleanup(t *testing.T) {
	s, task, _ := deepFixture(t)
	view := deepChecked(t, s, task)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE task_cost_attempts SET state='complete',booked_micros=0,final_micros=0 WHERE task_id=?`, task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CleanupResourcePaths(t.Context(), "scan", task.ID, "fixture-cleanup"); !errors.Is(e, ErrStorageProtected) {
		t.Fatal("fresh native checkpoint proof not protected", e)
	}
	if _, e := s.db.ExecContext(t.Context(), `UPDATE deep_resume_checks SET updated_at='2000-01-01T00:00:00Z' WHERE id=?`, *view.ProbeID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CleanupResourcePaths(t.Context(), "scan", task.ID, "fixture-cleanup"); e != nil {
		t.Fatal("expired read-only proof permanently blocked explicit cleanup", e)
	}
}
