package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestComponentPlansPipeline(t *testing.T) {
	root, _ := filepath.Abs("../../..")
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	secret, agentToken := "fixture-component-secret", "fixture-component-agent-token"
	if _, e := s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	limits := store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 3}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: limits}
	api := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
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
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Component planning fixture", Provider: "openai", Model: "fixture-planning-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	for _, name := range []string{"src/api/a.ts", "src/web/b.ts", "README.md", ".ignored", "源码/证据.ts"} {
		f := filepath.Join(source, name)
		if e = os.MkdirAll(filepath.Dir(f), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(f, []byte("Fixtures / Mock only\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Mkdir(filepath.Join(source, "empty"), 0700); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture-components.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	var full contracts.ComponentPlanDocument
	_ = json.Unmarshal([]byte(`{"components":[{"name":" Complete source ","paths":[".","."]}]}`), &full)
	create := func(document contracts.ComponentPlanDocument) contracts.ComponentPlanSnapshot {
		t.Helper()
		code, raw := doJSON(t, ts.URL+"/api/v1/component-plans", token, map[string]any{"snapshotId": snap.ID, "title": "Fixtures / Mock 组件计划", "document": document})
		if code != 200 {
			t.Fatal(code, raw)
		}
		var v struct {
			Data contracts.ComponentPlanSnapshot
		}
		if e := json.Unmarshal([]byte(raw), &v); e != nil {
			t.Fatal(e)
		}
		return v.Data
	}
	operation := func(plan contracts.ComponentPlanSnapshot, kind string) *store.Task {
		t.Helper()
		body := map[string]any{"revision": plan.Revision, "kind": kind}
		if kind == "suggest" {
			body["modelConfigId"] = cfg.ID
			body["acknowledgeUnknownCost"] = true
		}
		code, raw := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/operations", token, body)
		if code != 200 {
			t.Fatal(code, raw)
		}
		var v struct{ Data store.Task }
		_ = json.Unmarshal([]byte(raw), &v)
		return &v.Data
	}
	run := func(task *store.Task) *contracts.ComponentPlanningResult {
		t.Helper()
		runInputAgent(t, agent, root, ts.URL, agentToken, filepath.Join(scratch, "capture.json"))
		v, status, e := s.ComponentOperationResult(t.Context(), task.ID)
		if e != nil || v == nil {
			current, _ := s.Get(t.Context(), task.ID)
			t.Fatal("component result missing", e, status, current)
		}
		return v
	}
	freeze := func(plan contracts.ComponentPlanSnapshot, task *store.Task, result *contracts.ComponentPlanningResult) (int, string) {
		t.Helper()
		return doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/freeze", token, map[string]any{"revision": plan.Revision, "taskId": fmt.Sprint(task.ID), "resultSha256": store.ComponentResultDigest(result)})
	}
	plan := create(full)
	task := operation(plan, "normalize")
	old, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-old-component-agent", limits, contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}, contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}, contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5, 6}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}})
	if e != nil || old != nil {
		t.Fatal("old scanner accepted planning input", e, old)
	}
	if c, _ := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/operations", token, map[string]any{"revision": 1, "kind": "normalize"}); c != 409 {
		t.Fatal("duplicate in-flight operation", c)
	}
	result := run(task)
	if result.Error != nil || !result.Coverage.Valid || result.Coverage.FileCount != 5 || result.Cost.EstimatedUSD == nil || *result.Cost.EstimatedUSD != 0 || result.Document.Components[0].Name != "Complete source" || len(result.Document.Components[0].Paths) != 1 {
		t.Fatal("native normalize/coverage failed", result)
	}
	current, _ := s.Get(t.Context(), task.ID)
	jobRaw, e := os.ReadFile(filepath.Join(current.OutputPath, "runtime/job.json"))
	if e != nil {
		t.Fatal(e)
	}
	job, e := contracts.DecodeScannerJobV7(jobRaw, &result.Execution)
	if e != nil || string(job.Source.Path) == source || job.Source.ContentHash != contracts.Digest(hash) {
		t.Fatal("private execution snapshot missing", e, job.Source)
	}
	archived, e := s.AttemptResult(t.Context(), task.ID, 1)
	if e != nil || archived.Result == nil || len(archived.Result.Findings) != 0 {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), *archived.Completion); e != nil {
		t.Fatal("idempotent result", e)
	}
	forged := *archived.Completion
	bad := *archived.Result
	ext := map[string]json.RawMessage{}
	for k, v := range bad.Extensions.Value {
		ext[k] = v
	}
	changed := *result
	changed.Operation.Revision++
	ext["componentPlanning"], _ = contracts.EncodeJSON(changed)
	bad.Extensions.Value = ext
	forged.Result = contracts.Some(bad)
	if e = s.CompleteContractExecution(t.Context(), forged); e == nil {
		t.Fatal("foreign plan revision accepted")
	}
	code, raw := freeze(plan, task, result)
	if code != 200 {
		t.Fatal(code, raw)
	}
	var frozen struct {
		Data contracts.ComponentPlanVersion
	}
	_ = json.Unmarshal([]byte(raw), &frozen)
	code, second := freeze(plan, task, result)
	if code != 200 || second != raw {
		t.Fatal("freeze replay changed immutable version", code, second)
	}
	id, _ := strconv.ParseInt(string(plan.ID), 10, 64)
	actor, _ := strconv.ParseInt(string(frozen.Data.ConfirmedBy), 10, 64)
	edited := full
	edited.Components[0].Name = "Edited"
	next, e := s.SaveComponentDraft(t.Context(), id, 1, edited, actor, "manual")
	if e != nil || next.Revision != 2 {
		t.Fatal(e)
	}
	if _, e = s.SaveComponentDraft(t.Context(), id, 1, full, actor, "manual"); e == nil {
		t.Fatal("stale draft overwrite")
	}
	if c, _ := freeze(next, task, result); c != 409 {
		t.Fatal("old result froze new draft", c)
	}
	original, e := s.ComponentVersion(t.Context(), id, 1)
	if e != nil || original.Document.Components[0].Name != "Complete source" {
		t.Fatal("editing changed frozen version", e)
	}
	if _, e = s.CleanupResourcePaths(t.Context(), "snapshot", snap.ID, ""); e == nil {
		t.Fatal("bound source could be cleaned")
	}
	if e = s.DeleteScan(t.Context(), task.ID); e == nil {
		t.Fatal("validation evidence could be deleted")
	}
	if _, e = s.Retry(t.Context(), task.ID); e == nil {
		t.Fatal("plan operation replayed through scan retry")
	}
	for _, route := range []string{"report", "compare", "report.pdf"} {
		if c, _ := getJSON(t, fmt.Sprintf("%s/api/v1/scans/%d/%s", ts.URL, task.ID, route), token); c != 409 {
			t.Fatal("planning operation presented as audit", route, c)
		}
	}
	t.Run("coverage-faults", func(t *testing.T) {
		var invalid contracts.ComponentPlanDocument
		_ = json.Unmarshal([]byte(`{"components":[{"name":"Same","paths":["src","src/api"]},{"name":"Same","paths":["src/api/a.ts"]},{"name":"Empty","paths":["empty"]}]}`), &invalid)
		p := create(invalid)
		job := operation(p, "normalize")
		out := run(job)
		if out.Error != nil || out.Coverage.Valid || out.Coverage.OverlapCount != 3 || len(out.Coverage.UncoveredPaths) != 3 || len(out.Coverage.EmptyComponents) != 1 || len(out.Coverage.DuplicateNames) != 1 {
			t.Fatal("coverage faults lost", out)
		}
		if c, _ := freeze(p, job, out); c != 409 {
			t.Fatal("invalid coverage froze", c)
		}
	})
	t.Run("suggestions-cost-and-explicit-adoption", func(t *testing.T) {
		p := create(full)
		if c, _ := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(p.ID)+"/operations", token, map[string]any{"revision": 1, "kind": "suggest", "modelConfigId": cfg.ID}); c != 400 {
			t.Fatal("unknown cost acknowledgement missing", c)
		}
		job := operation(p, "suggest")
		out := run(job)
		if out.Error != nil || out.Cost.State != "unknown" || out.Cost.EstimatedUSD != nil {
			t.Fatal(out)
		}
		if c, _ := freeze(p, job, out); c != 409 {
			t.Fatal("unreviewed suggestion froze", c)
		}
		budget, e := s.BudgetSnapshot(t.Context(), job.ID)
		if e != nil || len(budget.UnknownAttempts) != 1 {
			t.Fatal("unknown fee lost", e, budget)
		}
		if _, e = s.Retry(t.Context(), job.ID); e == nil {
			t.Fatal("planning model replayed without new acknowledgement")
		}
		code, raw := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(p.ID)+"/apply-suggestion", token, map[string]any{"revision": 1, "taskId": fmt.Sprint(job.ID), "resultSha256": store.ComponentResultDigest(out)})
		if code != 200 {
			t.Fatal(code, raw)
		}
		var saved struct {
			Data contracts.ComponentPlanSnapshot
		}
		_ = json.Unmarshal([]byte(raw), &saved)
		if saved.Data.Revision != 2 || saved.Data.Origin != "sdk_suggested" {
			t.Fatal(saved)
		}
		capture, e := os.ReadFile(filepath.Join(scratch, "capture.json"))
		if e != nil || !strings.Contains(string(capture), `"fixtureOnly":true`) || strings.Contains(string(capture), `"leakedServerCredentials":true`) || strings.Contains(string(capture), `"source":"`+source+`"`) {
			t.Fatal("planning SDK isolation lost", e, string(capture))
		}
	})
	t.Run("late-suggestion-cannot-overwrite-new-draft", func(t *testing.T) {
		p := create(full)
		job := operation(p, "suggest")
		id, _ := strconv.ParseInt(string(p.ID), 10, 64)
		updated := full
		updated.Components = append([]contracts.ComponentPlanDocumentComponentsItem{}, full.Components...)
		updated.Components[0].Name = "Newer draft"
		next, e := s.SaveComponentDraft(t.Context(), id, p.Revision, updated, actor, "manual")
		if e != nil {
			t.Fatal(e)
		}
		out := run(job)
		if out.Operation.Revision != p.Revision || out.Error != nil {
			t.Fatal(out)
		}
		if c, _ := freeze(next, job, out); c != 409 {
			t.Fatal("late suggestion froze current draft", c)
		}
		if c, _ := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(p.ID)+"/apply-suggestion", token, map[string]any{"revision": p.Revision, "taskId": fmt.Sprint(job.ID), "resultSha256": store.ComponentResultDigest(out)}); c != 409 {
			t.Fatal("late suggestion overwrote current draft", c)
		}
		after, e := s.GetComponentPlan(t.Context(), id)
		if e != nil || after.Document.Components[0].Name != "Newer draft" {
			t.Fatal(e, after)
		}
	})
	for _, scenario := range []string{"failure", "mutate"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("AXIOM_COMPONENT_TEST_SCENARIO", scenario)
			p := create(full)
			job := operation(p, "suggest")
			out := run(job)
			if out.Error == nil || out.Document != nil || out.Coverage != nil || strings.Contains(out.Error.Message, "fixture-private-planning-token") {
				t.Fatal("invalid native suggestion accepted or secret leaked", out)
			}
			if c, _ := freeze(p, job, out); c != 409 {
				t.Fatal(c)
			}
		})
	}
	t.Run("snapshot-tamper-at-confirmation", func(t *testing.T) {
		p := create(full)
		job := operation(p, "normalize")
		out := run(job)
		file := filepath.Join(source, "README.md")
		before, _ := os.ReadFile(file)
		if e = os.WriteFile(file, []byte("tampered"), 0600); e != nil {
			t.Fatal(e)
		}
		defer os.WriteFile(file, before, 0600)
		if c, _ := freeze(p, job, out); c != 409 {
			t.Fatal("tampered source froze", c)
		}
		if _, e = s.CreateComponentPlan(t.Context(), snap.ID, "Tampered", full, actor); e == nil {
			t.Fatal("tampered source created a plan")
		}
	})
	t.Run("viewer-permissions", func(t *testing.T) {
		if c, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "component-fixture-viewer", "password": "fixture-password", "role": "viewer"}); c != 200 {
			t.Fatal(c, body)
		}
		viewer := login(t, ts.URL, "component-fixture-viewer", "fixture-password")
		if c, _ := getJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/versions/1", viewer); c != 200 {
			t.Fatal(c)
		}
		if c, _ := doJSON(t, ts.URL+"/api/v1/component-plans/"+string(plan.ID)+"/freeze", viewer, map[string]any{}); c != 403 {
			t.Fatal(c)
		}
	})
	t.Run("git-head-snapshot-binding", func(t *testing.T) {
		archive, base, head := gitPipelineArchive(t, t.TempDir())
		code, raw := uploadGitFixture(t, ts.URL, token, archive, base, head)
		if code != 200 {
			t.Fatal(code, string(raw))
		}
		var imported struct {
			Data struct{ Snapshot store.SnapshotRow }
		}
		if e := json.Unmarshal(raw, &imported); e != nil {
			t.Fatal(e)
		}
		p, e := s.CreateComponentPlan(t.Context(), imported.Data.Snapshot.ID, "Frozen HEAD", full, actor)
		if e != nil || p.Source.HeadCommit == nil || *p.Source.HeadCommit != head {
			t.Fatal("Git source identity missing", e, p.Source)
		}
		job := operation(p, "normalize")
		out := run(job)
		if out.Error != nil || !out.Coverage.Valid || out.Operation.Source.HeadCommit == nil || *out.Operation.Source.HeadCommit != head {
			t.Fatal("HEAD normalization binding lost", out)
		}
		if code, raw := freeze(p, job, out); code != 200 {
			t.Fatal(code, raw)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_COMPONENTS") == "1" {
		cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/component-plans-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_COMPONENT_SNAPSHOT="+fmt.Sprint(snap.ID), "AXIOM_TEST_COMPONENT_MODEL="+fmt.Sprint(cfg.ID), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal("Component Web acceptance", e, string(out))
		}
	}
	t.Run("expired-lease-never-replays", func(t *testing.T) {
		p := create(full)
		job := operation(p, "suggest")
		caps := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5, 6, 7}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
		input := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
		results := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
		claimed, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-expire", limits, input, results, caps)
		if e != nil || claimed == nil || claimed.ID != job.ID {
			t.Fatal(e, claimed)
		}
		db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		if _, e = db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, job.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(t.Context(), `UPDATE scan_attempts SET lease_expires_at='2000-01-01T00:00:00Z' WHERE task_id=$1`, job.ID); e != nil {
			t.Fatal(e)
		}
		claimed, e = s.ClaimExecutionWithObservations(t.Context(), "fixture-reaper", limits, input, results, caps)
		if e != nil || claimed != nil {
			t.Fatal("interrupted planner automatically replayed", e, claimed)
		}
		failed, e := s.Get(t.Context(), job.ID)
		if e != nil || failed.Status != "failed" || failed.Attempt != 1 {
			t.Fatal(e, failed)
		}
		if _, e = s.Retry(t.Context(), job.ID); e == nil {
			t.Fatal("interrupted planner reused through scan retry")
		}
	})
}
