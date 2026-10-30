package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"axiom/sourceidentity"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestComponentScansPipeline(t *testing.T) {
	s := inputPipelineStore(t)
	root, _ := filepath.Abs("../../..")
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("AXIOM_COMPONENT_SCAN_FIXTURE", "1")
	secret, agentToken := "fixture-components-secret", "fixture-components-agent"
	if _, e := s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	limits := store.ClaimLimits{Global: 3, PerModel: 3, PerProject: 3, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 3, TokenBudgetCapable: true, SDKConcurrencyCapable: true, SDKParametersCapable: true}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: limits}
	routes := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		if _, e := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); e == nil && r.URL.Path != "/" {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer ts.Close()
	token := login(t, ts.URL, "input-fixture-admin", "fixture-password")
	encrypted, e := cryptokey.Encrypt("fixture-component-key-not-real")
	if e != nil {
		t.Fatal(e)
	}
	price := inputFixturePrice()
	fixtureInput, fixtureOutput := contracts.TokenUnitPrice("5000"), contracts.TokenUnitPrice("15000")
	price.InputPerMillion, price.OutputPerMillion = &fixtureInput, &fixtureOutput
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: price, Name: "Component execution fixture", Provider: "openai", Model: "fixture-component-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	for _, p := range []string{"api/a.ts", "web/b.ts", "worker/c.ts"} {
		f := filepath.Join(source, p)
		os.MkdirAll(filepath.Dir(f), 0700)
		os.WriteFile(f, []byte("// Fixtures / Mock\n"), 0600)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-components.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	var document contracts.ComponentPlanDocument
	json.Unmarshal([]byte(`{"components":[{"name":"Fixtures / Mock API","paths":["api"]},{"name":"Fixtures / Mock Web","paths":["web"]},{"name":"Fixtures / Mock Worker","paths":["worker"]}]}`), &document)
	plan, e := s.CreateComponentPlan(t.Context(), snap.ID, "Fixtures / Mock component execution", document, 1)
	if e != nil {
		t.Fatal(e)
	}
	code, raw := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/operations", token, map[string]any{"revision": 1, "kind": "normalize"})
	if code != 200 {
		t.Fatal(code, raw)
	}
	var local struct{ Data store.Task }
	json.Unmarshal([]byte(raw), &local)
	runInputAgent(t, agent, root, ts.URL, agentToken, filepath.Join(scratch, "normalize.json"))
	normal, status, e := s.ComponentOperationResult(t.Context(), local.Data.ID)
	if e != nil || status != "completed" || normal == nil {
		t.Fatal(e, status)
	}
	frozen, e := s.FreezeComponentPlan(t.Context(), int64From(plan.ID), 1, local.Data.ID, 1, store.ComponentResultDigest(normal))
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("AXIOM_TEST_WEB_COMPONENT_SCANS") == "1" {
		t.Run("sdk-parameters-web-to-sdk", func(t *testing.T) {
			runSDKParametersWebAcceptance(t, s, agent, root, ts.URL, token, agentToken, scratch, snap.ID, plan.ID)
		})
	}
	launch := func() int64 {
		t.Helper()
		body := map[string]any{"version": frozen.Version, "versionSha256": frozen.SHA256, "modelConfigId": cfg.ID, "workers": 2, "timeoutMinutes": 5, "maxCostUsd": 6, "maxTotalCostUsd": 10, "componentCostUsd": 2, "acknowledgeMatchingCost": true}
		code, raw := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/scans", token, body)
		if code != 200 {
			t.Fatal(code, raw)
		}
		var v struct{ Data struct{ ID int64 } }
		json.Unmarshal([]byte(raw), &v)
		return v.Data.ID
	}
	run := func(id int64, scenario string) (*store.Task, *contracts.ComponentScanResult) {
		t.Helper()
		t.Setenv("AXIOM_COMPONENT_SCAN_SCENARIO", scenario)
		capture := filepath.Join(scratch, fmt.Sprintf("capture-%d-%s", id, scenario))
		runInputAgent(t, agent, root, ts.URL, agentToken, capture)
		task, e := s.Get(t.Context(), id)
		if e != nil {
			t.Fatal(e)
		}
		archive, e := s.AttemptResult(t.Context(), id, task.Attempt)
		if e != nil || archive.Result == nil {
			t.Fatal("component result not committed", e, task.Status, task.ErrorMessage)
		}
		r, e := contracts.DecodeComponentScanResult(archive.Result.Extensions.Value["componentScan"], &archive.Result.Execution)
		if e != nil {
			t.Fatal(e)
		}
		return task, &r
	}
	t.Run("frozen-source-result-cost-matching-and-identity", func(t *testing.T) {
		id := launch()
		task, result := run(id, "normal")
		if task.Status != "completed" || len(result.Snapshot.Components) != 3 || result.Snapshot.Matching.CostState != "unknown" {
			t.Fatal(task.Status, result)
		}
		budget, e := s.BudgetSnapshot(t.Context(), id)
		if e != nil || budget.State != "unknown" || budget.EstimatedMicros == nil || *budget.EstimatedMicros != 375000 || budget.RetryAllowed {
			t.Fatal("unknown matching was settled", e, budget)
		}
		archive, e := s.AttemptResult(t.Context(), id, 1)
		if e != nil || len(archive.Result.Findings) != 3 {
			t.Fatal(e)
		}
		if e = s.CompleteContractExecution(t.Context(), *archive.Completion); e != nil {
			t.Fatal("completion replay", e)
		}
		forged := *archive.Completion
		body := *archive.Result
		ext := map[string]json.RawMessage{}
		for k, v := range body.Extensions.Value {
			ext[k] = v
		}
		changed := *result
		changed.Request = result.Request
		changed.Request.GrantMicros++
		ext["componentScan"], _ = json.Marshal(changed)
		body.Extensions = contracts.Some(contracts.JSONObject(ext))
		forged.Result = contracts.Some(body)
		if e = s.CompleteContractExecution(t.Context(), forged); e == nil {
			t.Fatal("foreign allocation accepted")
		}
		if _, e = s.Retry(t.Context(), id); e == nil {
			t.Fatal("generic retry bypassed component selection")
		}
	})
	t.Run("partial-retry-only-failed-components-and-retain-history", func(t *testing.T) {
		id := launch()
		task, result := run(id, "partial")
		if task.Status != "partial" || result.Snapshot.Components[1].Status != "failed" || result.Snapshot.Components[1].FinalMicros != nil {
			t.Fatal(task.Status, result)
		}
		endpoint := fmt.Sprintf("%s/api/v1/scans/%d/component-executions/retry", ts.URL, id)
		body := map[string]any{"attempt": 1, "expectedStatus": "partial", "indices": []int{1}, "acknowledgeReexecution": true}
		if code, _ := doJSON(t, endpoint, token, body); code != 409 {
			t.Fatal("unknown cost retry was not blocked", code)
		}
		budget, _ := s.BudgetSnapshot(t.Context(), id)
		if e = s.ReconcileAttemptCost(t.Context(), id, 1, 0.5, "Fixtures / Mock checked fees", "1", budget.Revision); e != nil {
			t.Fatal(e)
		}
		body["indices"] = []int{0}
		if code, _ := doJSON(t, endpoint, token, body); code != 409 {
			t.Fatal("completed component replay accepted", code)
		}
		body["indices"] = []int{1}
		if code, raw := doJSON(t, endpoint, token, body); code != 200 {
			t.Fatal(code, raw)
		}
		next, out := run(id, "normal")
		if next.Attempt != 2 || len(out.Request.SelectedIndices) != 1 || out.Request.SelectedIndices[0] != 1 || out.Snapshot.Components[0].Index != 1 || out.Snapshot.Matching.CostState != "known" {
			t.Fatal(next, out)
		}
		history, e := s.ComponentAttempts(t.Context(), id)
		if e != nil || len(history) != 2 || history[1].Snapshot.Components[0].Status != "completed" {
			t.Fatal("completed history lost", e, history)
		}
		budget, _ = s.BudgetSnapshot(t.Context(), id)
		if budget.EstimatedMicros == nil || *budget.EstimatedMicros != 625000 {
			t.Fatal("retry double counted or discarded fees", budget)
		}
		first, e := s.AttemptResult(t.Context(), id, 1)
		if e != nil || len(first.Result.Findings) != 2 {
			t.Fatal("historical findings disappeared", e)
		}
		for _, test := range []struct {
			query string
			code  int
			api   bool
		}{
			{"", 200, false}, {"?attempt=1", 200, true}, {"?attempt=2", 200, false},
			{"?attempt=0", 400, false}, {"?attempt=01", 400, false},
			{"?attempt=1&attempt=2", 400, false}, {"?attempt=999", 404, false},
		} {
			code, raw := getJSON(t, fmt.Sprintf("%s/api/v1/scans/%d/report%s", ts.URL, id, test.query), token)
			if code != test.code || (code == 200 && strings.Contains(string(raw), "Fixtures / Mock API · api") != test.api) {
				t.Fatalf("report scope %s: status %d, body %s", test.query, code, raw)
			}
			if code == 200 && !strings.Contains(string(raw), "Fixtures / Mock Web · web") {
				t.Fatal("report lost selected component", test.query)
			}
		}
		stale := map[string]any{"attempt": 1, "expectedStatus": "partial", "indices": []int{1}, "acknowledgeReexecution": true}
		if code, _ := doJSON(t, endpoint, token, stale); code != 409 {
			t.Fatal("stale retry accepted", code)
		}
	})

	t.Run("parent-remains-partial-until-all-components-complete", func(t *testing.T) {
		id := launch()
		_, out := run(id, "partial-two")
		if out.Snapshot.Components[1].Status != "failed" || out.Snapshot.Components[2].Status != "failed" {
			t.Fatal(out)
		}
		b, _ := s.BudgetSnapshot(t.Context(), id)
		if e := s.ReconcileAttemptCost(t.Context(), id, 1, 0.5, "Fixture only fee reconciliation", "1", b.Revision); e != nil {
			t.Fatal(e)
		}
		changed, e := s.RetryComponents(t.Context(), id, 1, "partial", []int64{1})
		if e != nil || !changed {
			t.Fatal(e)
		}
		task, _ := run(id, "normal")
		if task.Status != "partial" {
			t.Fatal("unfinished parent incorrectly completed", task.Status)
		}
		a, e := s.AttemptResult(t.Context(), id, 2)
		if e != nil || a.Completion.ExecutionStatus != "completed" {
			t.Fatal("batch completion was overwritten by parent status", e)
		}
		// This single-component retry has complete scoped usage and no matching
		// request. Its frozen-price estimate is known even while the parent still
		// has an unfinished component; do not overwrite it with a manual amount.
		b, _ = s.BudgetSnapshot(t.Context(), id)
		var second *contracts.AttemptCost
		for i := range b.Attempts {
			if b.Attempts[i].Execution.Attempt == 2 {
				second = &b.Attempts[i]
			}
		}
		if second == nil || second.State != "complete" || second.FinalMicros == nil || *second.FinalMicros != 125000 || !second.TokenEstimate.Present || second.TokenEstimate.Value.Status != "estimated" {
			t.Fatal("complete retry usage was treated as an unknown whole-parent fee", b)
		}
		if e := s.ReconcileAttemptCost(t.Context(), id, 2, 0.5, "Fixture only fee reconciliation", "1", b.Revision); !errors.Is(e, store.ErrBudgetConflict) {
			t.Fatal("a known estimate could be overwritten manually", e)
		}
		changed, e = s.RetryComponents(t.Context(), id, 2, "partial", []int64{2})
		if e != nil || !changed {
			t.Fatal(e)
		}
		task, _ = run(id, "normal")
		if task.Status != "completed" {
			t.Fatal("parent did not complete after final component", task.Status)
		}
	})
	caps := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	results := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	observations := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5, 6, 7, 8}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	t.Run("old-node-unbounded-admission-reservation-and-expired-lease", func(t *testing.T) {
		id := launch()
		second := launch()
		old := observations
		old.JobVersions = []int64{3, 4, 5, 6, 7}
		// Budgeted claims require an explicit current runtime advertisement.
		for _, node := range []string{"fixture-old-component", "fixture-component-node", "fixture-component-node-2", "fixture-next-node", "fixture-expiry-drain"} {
			advertised := observations
			if node == "fixture-old-component" {
				advertised = old
			}
			if e := s.ReportAgentWithObservations(t.Context(), node, 1, caps, results, advertised); e != nil {
				t.Fatal(e)
			}
			version, build := int64(15), "fixtures-component-budget"
			apis := control.ControlAPIs()
			if e := s.ReportAgentRuntime(t.Context(), node, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixtures", ScannerBuild: &build, ControlAPI: int64(apis[len(apis)-1]), State: "ready", MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}); e != nil {
				t.Fatal(e)
			}
		}
		claimed, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-old-component", limits, caps, results, old)
		if e != nil || claimed != nil {
			t.Fatal("old node claimed new format", e)
		}
		claimed, e = s.ClaimExecutionWithObservations(t.Context(), "fixture-component-node", limits, caps, results, observations)
		if e != nil || claimed == nil || claimed.ID != id {
			t.Fatal(e, claimed)
		}
		v, _ := s.BudgetSnapshot(t.Context(), id)
		if v.ReservedMicros == nil || *v.ReservedMicros != 6000000 {
			t.Fatal("parent grant not reserved", v)
		}
		if another, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-component-node-2", limits, caps, results, observations); e != nil || another == nil || another.ID != second {
			t.Fatal("retired weighted quota blocked an independent component task", e, another)
		}
		if _, e = db.Exec(`UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, id); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`UPDATE scan_attempts SET lease_expires_at='2000-01-01T00:00:00Z' WHERE task_id=$1`, id); e != nil {
			t.Fatal(e)
		}
		other, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-next-node", limits, caps, results, observations)
		if e != nil || other != nil {
			t.Fatal(e, other)
		}
		expired, _ := s.Get(t.Context(), id)
		if expired.Status != "failed" || expired.Attempt != 1 {
			t.Fatal("lost component execution was replayed", expired)
		}
		// Finish the second manually claimed lease as interrupted, without calling SDK.
		s.RequestCancel(t.Context(), second)
		db.Exec(`UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, second)
		db.Exec(`UPDATE scan_attempts SET lease_expires_at='2000-01-01T00:00:00Z' WHERE task_id=$1`, second)
		s.ClaimExecutionWithObservations(t.Context(), "fixture-expiry-drain", limits, caps, results, observations)
	})
	t.Run("cancellation-keeps-known-lower-bound-and-unknown-final", func(t *testing.T) {
		id := launch()
		t.Setenv("AXIOM_COMPONENT_SCAN_SCENARIO", "hold")
		capture := filepath.Join(scratch, "cancel-capture")
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		cmd := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if data, _ := os.ReadFile(capture); len(data) > 0 {
				break
			}
			if time.Now().After(deadline) {
				cmd.Process.Kill()
				t.Fatal("fixture component did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if _, e := s.RequestCancel(t.Context(), id); e != nil {
			t.Fatal(e)
		}
		if e := cmd.Wait(); e != nil {
			t.Fatal(e)
		}
		task, _ := s.Get(t.Context(), id)
		if task.Status != "canceled" {
			t.Fatal(task)
		}
		v, _ := s.BudgetSnapshot(t.Context(), id)
		if v.State != "unknown" {
			t.Fatal("cancel final cost was inferred", v)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_COMPONENT_SCANS") == "1" {
		t.Setenv("AXIOM_COMPONENT_SCAN_SCENARIO", "normal")
		if _, e := s.CreateAccount(t.Context(), "component-scan-viewer", auth.HashPassword(secret, "fixture-password"), "viewer"); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/component-scans-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_COMPONENT_PLAN="+string(plan.ID), "AXIOM_TEST_COMPONENT_MODEL="+fmt.Sprint(cfg.ID), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		} else {
			t.Log(string(out))
		}
	}
}
func int64From(v contracts.TaskID) int64 { var n int64; fmt.Sscan(string(v), &n); return n }
