package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresNativeConcurrencyDispatch(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	snapshot, e := s.InsertSnapshot(ctx, SnapshotRow{OriginalFilename: "fixture-native.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: strings.Repeat("b", 64), SnapshotPath: "/fixture/source"})
	if e != nil {
		t.Fatal(e)
	}
	model := contracts.ResolvedModel{ProviderID: "openai", ConnectionType: "openai", Model: "fixture-native-model", WireAPI: "responses", CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "fixture-native"}
	m, _ := json.Marshal(model)
	input := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	result := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	observation := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 8}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	limits := ClaimLimits{Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2, SDKConcurrencyCapable: true}
	node := "fixture-native-node"
	if e = s.ReportAgentWithObservations(ctx, node, 129, input, result, observation); e != nil {
		t.Fatal(e)
	}
	build := "fixture-scanner"
	report := func(version int64, stale bool) {
		t.Helper()
		if e = s.ReportAgentRuntime(ctx, node, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixture", ControlAPI: 4, State: "ready", ScannerBuild: &build, MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}); e != nil {
			t.Fatal(e)
		}
		if stale {
			_, e = s.db.ExecContext(ctx, `UPDATE execution_agents SET runtime_seen_at=NOW()-INTERVAL '1 minute' WHERE agent_id=?`, node)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	for i, kind := range []string{"workers", "discovery", "stop", "subagents", "safe-max"} {
		policy := contracts.ScanPolicy{Mode: "deep", TimeBudget: contracts.TimeBudget{ScanSeconds: 2820, FinalizationGraceSeconds: 600}, IncludePaths: []contracts.RelativePath{"api/a.ts"}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}, MaxCostUSD: contracts.Some(3.0)}
		task := Task{ScanNo: fmt.Sprintf("fixture-native-%d", i), SourcePath: snapshot.SnapshotPath, SourceSnapshotID: snapshot.ID, SourceContentHash: snapshot.ContentHash, Provider: "openai", Model: model.Model, ScanMode: "deep", TimeoutMinutes: 47, InputContractVersion: 1, ResultContractVersion: 1, ObservationContractVersion: 1, ModelSnapshotJSON: string(m), IncludePaths: `["api/a.ts"]`}
		budget := 3.0
		task.MaxCostUSD = &budget
		switch kind {
		case "workers", "safe-max":
			v := 129
			if kind == "safe-max" {
				v = contracts.SDKMaxSafeInteger
			}
			task.Workers = &v
			policy.Workers = contracts.Some(int64(v))
		case "discovery":
			task.DeepMaxDiscoveryRuns = 1001
			policy.DeepMaxDiscoveryRuns = contracts.Some(int64(1001))
		case "stop":
			task.DeepStopAfterNoNew = 1002
			policy.DeepStopAfterNoNew = contracts.Some(int64(1002))
		case "subagents":
			task.InputContractVersion = contracts.ScanInputVersion
			task.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}, Subagents: contracts.Some(int64(65))}
		}
		p, _ := json.Marshal(policy)
		task.PolicySnapshotJSON = string(p)
		id, e := s.InsertPending(ctx, task)
		if e != nil {
			t.Fatal(kind, e)
		}
		for _, v := range []struct {
			version  int64
			stale    bool
			declared bool
		}{{11, false, true}, {12, true, true}, {12, false, false}} {
			report(v.version, v.stale)
			l := limits
			l.SDKConcurrencyCapable = v.declared
			claimed, e := s.ClaimExecutionWithObservations(ctx, node, l, input, result, observation)
			if e != nil || claimed != nil {
				t.Fatal("old, missing or stale capability claimed new range", kind, claimed, e)
			}
		}
		report(11, false)
		overview, e := s.QueueOverview(ctx, limits, id)
		if e != nil || len(overview.Tasks) != 1 || overview.Tasks[0].Reason != "sdk_concurrency_incompatible" {
			t.Fatal("missing compatibility diagnosis", e, overview.Tasks)
		}
		report(12, false)
		claimed, e := s.ClaimExecutionWithObservations(ctx, node, limits, input, result, observation)
		if e != nil || claimed == nil || claimed.ID != id || claimed.TimeoutMinutes != 47 || *claimed.MaxCostUSD != 3 || claimed.IncludePaths != task.IncludePaths || claimed.PolicySnapshotJSON != task.PolicySnapshotJSON {
			t.Fatal("native values or operator boundaries changed", kind, claimed, e)
		}
	}
	for _, kind := range []string{"api_key", "chatgpt"} {
		t.Run("parallel-shared-"+kind, func(t *testing.T) {
			identity, e := s.CreateSDKIdentity(ctx, "Fixtures / Mock shared "+kind, node, kind)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.db.ExecContext(ctx, `UPDATE sdk_identities SET state='authenticated',authenticated=TRUE WHERE id=?`, identity.Identity.ID); e != nil {
				t.Fatal(e)
			}
			policy := contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
			p, _ := json.Marshal(policy)
			ids := []int64{}
			for i := 0; i < 2; i++ {
				id, e := s.InsertPending(ctx, Task{ScanNo: fmt.Sprintf("fixture-shared-%s-%d", kind, i), SourcePath: snapshot.SnapshotPath, SourceSnapshotID: snapshot.ID, SourceContentHash: snapshot.ContentHash, Provider: "openai", Model: model.Model, ScanMode: "standard", TimeoutMinutes: 1, InputContractVersion: 1, ResultContractVersion: 1, ObservationContractVersion: 1, ModelSnapshotJSON: string(m), PolicySnapshotJSON: string(p), SDKIdentity: &identity.Identity})
				if e != nil {
					t.Fatal(e)
				}
				ids = append(ids, id)
			}
			l := limits
			l.SDKIdentityCapable = true
			first, e := s.ClaimExecutionWithObservations(ctx, node, l, input, result, observation)
			if e != nil || first == nil || first.ID != ids[0] {
				t.Fatal(first, e)
			}
			old := l
			old.SDKConcurrencyCapable = false
			if claimed, e := s.ClaimExecutionWithObservations(ctx, node, old, input, result, observation); e != nil || claimed != nil {
				t.Fatal("old runtime bypassed identity isolation", claimed, e)
			}
			second, e := s.ClaimExecutionWithObservations(ctx, node, l, input, result, observation)
			if e != nil || second == nil || second.ID != ids[1] {
				t.Fatal("shared identity retains a whole-task concurrency cap", second, e)
			}
			if _, e = s.NewSDKIdentityOperation(ctx, int64FromFixture(identity.Identity.ID), identity.Identity.Generation, "account", ""); e == nil {
				t.Fatal("account operation raced active credential snapshots")
			}
		})
	}
}

func int64FromFixture(value string) int64 { var result int64; fmt.Sscan(value, &result); return result }

func TestPostgresNativeConcurrencyMigrationPreservesHistory(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	id, e := s.InsertPending(ctx, Task{ScanNo: "fixture-history-before-024", SourcePath: "/fixture/source", Provider: "openai", Model: "fixture-model", ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReplaceFindings(ctx, id, []Finding{{Fingerprint: "fixture-history-evidence", Title: "Fixtures / Mock preserved evidence", Severity: "low", Description: "Not a real finding"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.ReportAgent(ctx, "fixture-legacy-presence", 64); e != nil {
		t.Fatal(e)
	}
	before, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	// Recreate the previous numeric constraints in this disposable test database,
	// then let the production migration runner upgrade it with existing evidence.
	for _, sql := range []string{
		`ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_workers_check`,
		`ALTER TABLE scan_tasks ALTER COLUMN workers TYPE INTEGER`,
		`ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_workers_check CHECK(workers BETWEEN 1 AND 64)`,
		`ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_deep_max_discovery_runs_check`,
		`ALTER TABLE scan_tasks ALTER COLUMN deep_max_discovery_runs TYPE INTEGER`,
		`ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_deep_max_discovery_runs_check CHECK(deep_max_discovery_runs BETWEEN 0 AND 1000)`,
		`ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_deep_stop_after_no_new_check`,
		`ALTER TABLE scan_tasks ALTER COLUMN deep_stop_after_no_new TYPE INTEGER`,
		`ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_deep_stop_after_no_new_check CHECK(deep_stop_after_no_new BETWEEN 0 AND 1000)`,
		`ALTER TABLE component_scans DROP CONSTRAINT component_scans_workers_check`,
		`ALTER TABLE component_scans ALTER COLUMN workers TYPE INTEGER`,
		`ALTER TABLE component_scans ADD CONSTRAINT component_scans_workers_check CHECK(workers BETWEEN 1 AND 8)`,
		`ALTER TABLE execution_agents DROP CONSTRAINT execution_agents_slots_check`,
		`ALTER TABLE execution_agents ALTER COLUMN slots TYPE INTEGER`,
		`ALTER TABLE execution_agents ADD CONSTRAINT execution_agents_slots_check CHECK(slots BETWEEN 0 AND 64)`,
		`DELETE FROM schema_migrations WHERE version='024_sdk_native_concurrency.sql'`,
	} {
		if _, e = s.db.ExecContext(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	next, e := Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	after, e := next.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("history changed during widening")
	}
	findings, e := next.ListFindings(ctx, id)
	if e != nil || len(findings) != 1 || findings[0].Fingerprint != "fixture-history-evidence" {
		t.Fatal("evidence lost", e, findings)
	}
	for _, column := range []string{"workers", "deep_max_discovery_runs", "deep_stop_after_no_new"} {
		var kind string
		if e = next.db.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_name='scan_tasks' AND column_name=?`, column).Scan(&kind); e != nil || kind != "bigint" {
			t.Fatal("integer width not upgraded", column, kind, e)
		}
	}
	if e = next.ReportAgent(ctx, "fixture-legacy-presence", contracts.SDKMaxSafeInteger); e != nil {
		t.Fatal("legacy metadata retains an extra limit", e)
	}
}
