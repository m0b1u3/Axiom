package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"errors"
	"testing"
	"time"
)

func richPresence() AgentPresence {
	version := int64(15)
	return AgentPresence{Runtime: &contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "Fixtures-Mock", ControlAPI: 4, State: "ready", MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}, AgentID: "fixture", Online: true, InputCapabilities: contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}, ResultCapabilities: contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}, ObservationCapabilities: contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}}
}

func TestPostgresRuntimeAdmissionAndStaleDiagnostics(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	if _, e := s.db.ExecContext(ctx, `UPDATE scan_tasks SET observation_contract_version=1 WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	a := richPresence()
	build := "fixture-scanner"
	description := int64(4)
	runtime := contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixture-agent", ScannerBuild: &build, ScannerDescriptionVersion: &description, ControlAPI: 4, State: contracts.AgentRuntimeStateReady, MatrixHash: control.MatrixHash}
	if e := s.ReportAgentRuntime(ctx, "unknown", runtime); !errors.Is(e, ErrAgentNotRegistered) {
		t.Fatal("unregistered runtime accepted", e)
	}
	if e := s.ReportAgentWithObservations(ctx, a.AgentID, 1, a.InputCapabilities, a.ResultCapabilities, a.ObservationCapabilities); e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"blocked", "matrix", "control"} {
		candidate := runtime
		switch state {
		case "blocked":
			candidate.State = contracts.AgentRuntimeStateBlocked
			candidate.Diagnostic = &contracts.ContractError{Code: "protocol_version_unsupported", Origin: "agent", Message: "Fixture preflight blocked", Retryable: false}
		case "matrix":
			candidate.MatrixHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "control":
			candidate.ControlAPI = 2
		}
		if e := s.ReportAgentRuntime(ctx, a.AgentID, candidate); e != nil {
			t.Fatal(e)
		}
		if task, e := s.ClaimExecutionWithObservations(ctx, a.AgentID, limits, a.InputCapabilities, a.ResultCapabilities, a.ObservationCapabilities); e != nil || task != nil {
			t.Fatal("runtime incompatible node admitted", state, task, e)
		}
		q, e := s.QueueOverview(ctx, limits, id)
		if e != nil || q.Tasks[0].Compatibility.Candidates[0].Ready || q.Agents[0].RuntimeReportedAt == nil {
			t.Fatal("runtime mismatch not observable", state, q, e)
		}
	}
	// An older process reusing an ID only publishes presence. Its fresh presence
	// cannot preserve a previous build's runtime block beyond the report's TTL.
	if _, e := s.db.ExecContext(ctx, `UPDATE execution_agents SET runtime_seen_at=NOW()-INTERVAL '1 minute' WHERE agent_id=?`, a.AgentID); e != nil {
		t.Fatal(e)
	}
	if e := s.ReportAgentWithContracts(ctx, a.AgentID, 1, a.InputCapabilities, a.ResultCapabilities); e != nil {
		t.Fatal(e)
	}
	q, e := s.QueueOverview(ctx, limits, id)
	if e != nil || q.Agents[0].Runtime != nil || q.Tasks[0].Reason != "observation_incompatible" {
		t.Fatal("stale build diagnostic survived", q, e)
	}
	if e = s.ReportAgentWithObservations(ctx, a.AgentID, 1, a.InputCapabilities, a.ResultCapabilities, a.ObservationCapabilities); e != nil {
		t.Fatal(e)
	}
	if e = s.ReportAgentRuntime(ctx, a.AgentID, runtime); e != nil {
		t.Fatal(e)
	}
	if task, e := s.ClaimExecutionWithObservations(ctx, a.AgentID, limits, a.InputCapabilities, a.ResultCapabilities, a.ObservationCapabilities); e != nil || task == nil || task.ID != id {
		t.Fatal("compatible runtime could not dispatch", task, e)
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE scan_tasks SET observation_contract_version=9 WHERE id=?`, id); e == nil {
		t.Fatal("unknown contract version persisted")
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE scan_tasks SET status='pending',lease_expires_at=NULL,input_contract_version=0 WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if task, e := s.ClaimExecutionWithObservations(ctx, a.AgentID, limits, a.InputCapabilities, a.ResultCapabilities, a.ObservationCapabilities); e != nil || task != nil {
		t.Fatal("future task admitted", task, e)
	}
	q, e = s.QueueOverview(ctx, limits, id)
	if e != nil || q.Tasks[0].Reason != "task_contract_unsupported" || q.MaxRecoveries != limits.MaxRecoveries || q.ServerTime.Before(time.Now().Add(-time.Minute)) {
		t.Fatal(q, e)
	}
}
func TestCompatibilityReasonsUseWholeNodeAndPreserveUnknowns(t *testing.T) {
	task := QueueTask{inputVersion: 1, resultVersion: 1, observationVersion: 1, budget: true}
	a := richPresence()
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "" {
		t.Fatal(v)
	}
	a.InputCapabilities.MaxCostUSD = false
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "input_incompatible" || v.Candidates[0].Missing[0] != "budget" {
		t.Fatal(v)
	}
	a = richPresence()
	a.ObservationCapabilities = contracts.NoObservationCapabilities()
	b := richPresence()
	b.ResultCapabilities = contracts.NoResultCapabilities()
	if v := evaluateTaskCompatibility(task, []AgentPresence{a, b}); v.Reason != "observation_incompatible" {
		t.Fatal("combined different nodes", v)
	}
	a = richPresence()
	a.Runtime = &contracts.AgentRuntime{ScannerDescriptionVersion: func() *int64 { v := int64(15); return &v }(), State: contracts.AgentRuntimeStateBlocked, ControlAPI: 4, MatrixHash: control.MatrixHash}
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "scanner_incompatible" {
		t.Fatal(v)
	}
	a = richPresence()
	a.Runtime = &contracts.AgentRuntime{ScannerDescriptionVersion: func() *int64 { v := int64(15); return &v }(), State: contracts.AgentRuntimeStateReady, ControlAPI: 2, MatrixHash: control.MatrixHash}
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "control_incompatible" {
		t.Fatal(v)
	}
	a.Runtime.MatrixHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	a.Runtime.State = contracts.AgentRuntimeStateBlocked
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "matrix_incompatible" || len(v.Candidates[0].Missing) != 2 || v.Candidates[0].Missing[0] != "matrix" {
		t.Fatal("matrix mismatch confused with Scanner preflight", v)
	}
	task.observationVersion = 9
	if v := evaluateTaskCompatibility(task, []AgentPresence{richPresence()}); v.Reason != "task_contract_unsupported" {
		t.Fatal(v)
	}
}

func TestGitDiffQueueCompatibilityKeepsOperationCapabilitySeparate(t *testing.T) {
	a := richPresence()
	a.ObservationCapabilities.JobVersions = []int64{3, 4, 6}
	task := QueueTask{inputVersion: contracts.GitDiffInputVersion, resultVersion: 1, observationVersion: 1, scanInput: true}
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "" {
		t.Fatal("Git scan should be runnable", v)
	}
	task.sdkOperation = true
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason == "" || v.Candidates[0].Ready {
		t.Fatal("operation support invented", v)
	}
	a.ObservationCapabilities.JobVersions = append(a.ObservationCapabilities.JobVersions, 5)
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "" || !v.Required.SDKOperation {
		t.Fatal("Git operation requirements lost", v)
	}
	a.ObservationCapabilities.JobVersions = []int64{3, 4, 5}
	if v := evaluateTaskCompatibility(task, []AgentPresence{a}); v.Reason != "git_diff_incompatible" || v.Candidates[0].Ready {
		t.Fatal("old Scanner should wait", v)
	}
}
