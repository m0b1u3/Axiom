package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
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
	"sync"
	"testing"
	"time"
)

// Real Server, Agent and Scanner. Only the model SDK runtime is replaced.
func TestInterimReviewPipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner first")
	}
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	secret, agentToken := "fixture-interim-session", "fixture-interim-agent"
	_, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password"))
	if e != nil {
		t.Fatal(e)
	}
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	source := filepath.Join(scratch, "source")
	os.MkdirAll(filepath.Join(source, "src"), 0700)
	for _, name := range []string{"README.md", "src/query.ts", "src/routes.ts"} {
		raw, err := os.ReadFile(filepath.Join(root, "test-fixtures/interim-review-mini", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(source, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-mini-review.zip", ArchiveSha256: strings.Repeat("b", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	key, e := cryptokey.Encrypt("fixture-interim-only-key")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock intermediate", Provider: "openai", Model: "gpt-6-sol", ApiKeyEncrypted: key})
	if e != nil {
		t.Fatal(e)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Lease: time.Minute, OutputRoot: filepath.Join(scratch, "scans"), MaxRecoveries: 0, SDKConcurrencyCapable: true}}
	api := srv.Routes()
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
	task := knowledgeTask(t, ts.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "scanMode": "standard"})
	if code, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "interim-fixture-viewer", "password": "fixture-password", "role": "viewer"}); code != 200 {
		t.Fatal(code, body)
	}

	run := func(name, scenario string) {
		capture := filepath.Join(scratch, name+".json")
		os.WriteFile(capture+".release", []byte("Fixtures / Mock release"), 0600)
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()
		command := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
		command.Env = append(command.Env, "AXIOM_RESULT_TEST_SCENARIO=interim-failure", "AXIOM_INTERIM_REVIEW_SOURCE=1", "AXIOM_OPERATION_TEST_SCENARIO="+scenario)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatal(name, err, string(out))
		}
	}
	run("saved-source", "reportable")
	get := func(path string, out any) {
		c, raw := getJSON(t, ts.URL+"/api/v1"+path, token)
		if c != 200 {
			t.Fatal(path, c, string(raw))
		}
		var v struct {
			Data json.RawMessage `json:"data"`
		}
		json.Unmarshal(raw, &v)
		if e := json.Unmarshal(v.Data, out); e != nil {
			t.Fatal(e, string(raw))
		}
	}
	post := func(path string, body any, out any, want int) {
		c, raw := doJSON(t, ts.URL+"/api/v1"+path, token, body)
		if c != want {
			t.Fatal(path, c, raw)
		}
		if want == 200 && out != nil {
			var v struct {
				Data json.RawMessage `json:"data"`
			}
			json.Unmarshal([]byte(raw), &v)
			if e := json.Unmarshal(v.Data, out); e != nil {
				t.Fatal(e, raw)
			}
		}
	}
	base := fmt.Sprintf("/scans/%d", task.ID)
	var page contracts.InterimPage
	get(base+"/attempts/1/interim", &page)
	if page.Total != 3 {
		t.Fatal(page.Total)
	}
	get(base+"/attempts/1/interim?duplicates=collapse", &page)
	if page.Total != 2 {
		t.Fatal("exact collapse", page.Total)
	}
	get(base+"/attempts/1/interim?duplicates=only", &page)
	if page.Total != 2 {
		t.Fatal("exact duplicates", page.Total)
	}
	get(base+"/attempts/1/interim?severity=low", &page)
	if page.Total != 0 {
		t.Fatal("severity filter", page.Total)
	}
	if os.Getenv("AXIOM_TEST_WEB_INTERIM_REVIEW") == "1" {
		browser := exec.Command("node", "tests/interim-review-web-acceptance.mjs")
		browser.Dir = filepath.Join(root, "code-audit-web")
		browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_INTERIM_TASK="+fmt.Sprint(task.ID), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
		if out, e := browser.CombinedOutput(); e != nil {
			t.Fatal("candidate review browser", e, string(out))
		} else {
			t.Log(string(out))
		}
		return
	}
	get(base+"/attempts/1/interim", &page)
	request := func(c contracts.InterimCandidate, n int) contracts.InterimReviewRequest {
		return contracts.InterimReviewRequest{SchemaVersion: 1, RequestID: fmt.Sprintf("12345678-1234-4123-8123-%012d", n), Members: []contracts.InterimReviewSelection{{Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}, CandidateID: c.ID, ArtifactSHA256: c.Artifact}}, Grouping: "exact", GroupingReason: "", AcknowledgeUnknownCost: true}
	}
	type created struct {
		TaskID  int64 `json:"taskId"`
		GroupID int64 `json:"groupId"`
	}
	bad := request(page.Items[0], 1)
	bad.AcknowledgeUnknownCost = false
	post(base+"/interim-reviews", bad, nil, 400)
	bad = request(page.Items[0], 1)
	bad.Members[0].ArtifactSHA256 = contracts.Digest(strings.Repeat("f", 64))
	post(base+"/interim-reviews", bad, nil, 409)
	var first created
	post(base+"/interim-reviews", request(page.Items[0], 2), &first, 200)
	// An identical record at another pointer maps to the same exact group and
	// associates its distinct original evidence reference without another model.
	var duplicate created
	post(base+"/interim-reviews", request(page.Items[1], 3), &duplicate, 200)
	if first != duplicate {
		t.Fatal("exact duplicate created another model task", first, duplicate)
	}
	var same created
	post(base+"/interim-reviews", request(page.Items[0], 2), &same, 200)
	if same != first {
		t.Fatal("request replay", same, first)
	}
	run("first-review", "reportable")
	var view store.InterimReviewView
	get(fmt.Sprintf("/interim-review-groups/%d", first.GroupID), &view)
	h := view.History[0]

	budget, e := s.BudgetSnapshot(t.Context(), first.TaskID)
	if e != nil || budget.AccountingVersion != 1 || len(budget.UnknownAttempts) != 1 {
		t.Fatal("independent review budget missing", e)
	}
	originalResult, e := s.AttemptResult(t.Context(), first.TaskID, h.Attempt)
	if e != nil || originalResult.Completion == nil {
		t.Fatal(e)
	}
	copyBytes, _ := contracts.EncodeJSON(*originalResult.Completion)
	foreign, _ := contracts.DecodeExecutionCompletion(copyBytes, &originalResult.Completion.Execution)
	corrupted := *h.Outcome
	corrupted.ReportSHA256 = contracts.Digest(strings.Repeat("f", 64))
	value, _ := contracts.EncodeJSON(corrupted)
	foreign.Result.Value.Extensions.Value["interimReviewOutcome"] = value
	if err := s.CompleteContractExecution(t.Context(), foreign); !errors.Is(err, store.ErrInvalidResult) {
		t.Fatal("foreign report accepted", err)
	}
	if h.Outcome == nil || h.Outcome.Reproduced || h.Outcome.Disposition != "reportable" || len(view.Sources) != 2 {
		t.Fatal("missing frozen review provenance", view)
	}
	captured, _ := os.ReadFile(filepath.Join(scratch, "first-review.json"))
	var payload struct {
		Options struct {
			Finding struct {
				SourceContentHash contracts.Digest `json:"sourceContentHash"`
				InputSHA256       string           `json:"inputSha256"`
				Candidates        []struct {
					Origins       []json.RawMessage `json:"origins"`
					EvidenceState string            `json:"evidenceState"`
				} `json:"candidates"`
				Documents json.RawMessage `json:"documents"`
			} `json:"finding"`
		}
		Leaked bool `json:"leakedServerCredentials"`
	}
	if e := json.Unmarshal(captured, &payload); e != nil || payload.Options.Finding.SourceContentHash != contracts.Digest(hash) || payload.Options.Finding.InputSHA256 != h.InputSHA256 || len(payload.Options.Finding.Candidates) != 1 || len(payload.Options.Finding.Candidates[0].Origins) != 1 || payload.Options.Finding.Candidates[0].EvidenceState != "selected_record" || payload.Options.Finding.Documents != nil || payload.Leaked {
		t.Fatal("SDK mapping", e, string(captured))
	}
	archive := store.InterimArchiveRequest{ReviewTaskID: first.TaskID, ReviewAttempt: h.Attempt, ReportSHA256: string(h.Outcome.ReportSHA256), Title: "Fixtures / Mock archived candidate", Severity: "high"}
	wrong := archive
	wrong.ReportSHA256 = strings.Repeat("f", 64)
	post(fmt.Sprintf("/interim-review-groups/%d/archive", first.GroupID), wrong, nil, 409)
	var finding store.Finding
	post(fmt.Sprintf("/interim-review-groups/%d/archive", first.GroupID), archive, &finding, 200)
	if finding.ID < 1 {
		t.Fatal("archive missing identity")
	}
	var again store.Finding
	post(fmt.Sprintf("/interim-review-groups/%d/archive", first.GroupID), archive, &again, 200)
	if again.ID != finding.ID {
		t.Fatal("archive replay duplicated finding")
	}
	result, e := s.AttemptResult(t.Context(), first.TaskID, h.Attempt)
	if e != nil || len(result.Findings) != 1 || len(result.Result.Findings) != 0 {
		t.Fatal("native result was changed or platform archive missing", e, result)
	}
	graphURL := fmt.Sprintf("/scans/%d/attempts/%d/evidence-graph?fingerprint=%s", first.TaskID, h.Attempt, finding.Fingerprint)
	var graph contracts.EvidenceGraph
	get(graphURL, &graph)
	for _, n := range graph.Nodes {
		if n.State == "verified" || n.State == "reproduced" {
			t.Fatal("reportable promoted to reproduction", n)
		}
	}
	if graph.Origin != "review_archive" || len(graph.Nodes) < 3 {
		t.Fatal("review graph lost candidate origin", graph)
	}
	if c, raw := getJSON(t, ts.URL+fmt.Sprintf("/api/v1/scans/%d/report?attempt=1", first.TaskID), token); c != 200 || !strings.Contains(string(raw), "未复现") {
		t.Fatal("platform archive report unavailable", c, string(raw))
	}
	current, _ := s.Get(t.Context(), task.ID)
	if current.Status != "failed" || current.FindingCount != 0 {
		t.Fatal("original failed execution changed", current)
	}
	if _, e = s.CleanupResourcePaths(t.Context(), "scan", first.TaskID, ""); !errors.Is(e, store.ErrStorageProtected) {
		t.Fatal("review artifacts not protected", e)
	}
	// A second, distinct group exercises suppressed/deferred/failure, immutable
	// retries after original derived files become unavailable, and stale reports.
	var second created
	post(base+"/interim-reviews", request(page.Items[2], 4), &second, 200)
	run("suppressed-review", "suppressed")
	get(fmt.Sprintf("/interim-review-groups/%d", second.GroupID), &view)
	h = view.History[0]
	archive.ReviewTaskID = second.TaskID
	archive.ReviewAttempt = h.Attempt
	archive.ReportSHA256 = string(h.Outcome.ReportSHA256)
	post(fmt.Sprintf("/interim-review-groups/%d/archive", second.GroupID), archive, nil, 409)

	manual := request(page.Items[0], 7)
	manual.Members = append(manual.Members, request(page.Items[2], 8).Members[0])
	post(base+"/interim-reviews", manual, nil, 409)
	manual.Grouping = "manual"
	post(base+"/interim-reviews", manual, nil, 409)
	originalAttempt, _ := s.GetAttempt(t.Context(), task.ID, 1)
	index := filepath.Join(originalAttempt.OutputPath, "checkpoints/interim/index.json")
	if e = os.Rename(index, index+".offline"); e != nil {
		t.Fatal(e)
	}
	defer os.Rename(index+".offline", index)
	retry := map[string]any{"requestId": "12345678-1234-4123-8123-000000000005", "acknowledgeUnknownCost": true}
	var retried created
	post(fmt.Sprintf("/interim-review-groups/%d/retry", second.GroupID), retry, &retried, 200)
	post(fmt.Sprintf("/interim-review-groups/%d/retry", second.GroupID), retry, &same, 200)
	if retried != same || retried.TaskID == second.TaskID {
		t.Fatal("frozen retry idempotency")
	}
	run("failed-review", "validation-failure")
	get(fmt.Sprintf("/interim-review-groups/%d", second.GroupID), &view)
	if view.History[0].Status != "failed" || len(view.History) != 2 || view.History[0].InputSHA256 != view.History[1].InputSHA256 {
		t.Fatal("failure or frozen retry provenance lost", view.History)
	}
	post(fmt.Sprintf("/interim-review-groups/%d/archive", second.GroupID), archive, nil, 409)
	retry["requestId"] = "12345678-1234-4123-8123-000000000006"
	post(fmt.Sprintf("/interim-review-groups/%d/retry", second.GroupID), retry, &retried, 200)
	run("deferred-review", "deferred")
	get(fmt.Sprintf("/interim-review-groups/%d", second.GroupID), &view)
	if view.History[0].Outcome.Disposition != "deferred" {
		t.Fatal(view)
	}
	archive.ReviewTaskID = retried.TaskID
	archive.ReportSHA256 = string(view.History[0].Outcome.ReportSHA256)
	post(fmt.Sprintf("/interim-review-groups/%d/archive", second.GroupID), archive, nil, 409)
	post(fmt.Sprintf("/interim-review-groups/%d/retry", second.GroupID), map[string]any{"requestId": strings.Repeat("x", 36), "acknowledgeUnknownCost": true}, nil, 400)
	if e = os.Rename(index+".offline", index); e != nil {
		t.Fatal(e)
	}
	manual.GroupingReason = "Fixtures / Mock：人工确认两个候选描述同一调用路径"
	var combined created
	post(base+"/interim-reviews", manual, &combined, 200)
	var combinedView store.InterimReviewView
	get(fmt.Sprintf("/interim-review-groups/%d", combined.GroupID), &combinedView)
	if combinedView.Input.Grouping != "manual" || len(combinedView.Input.Members) != 2 {
		t.Fatal("manual grouping provenance lost", combinedView)
	}
	run("manual-bad-source", "reportable-bad-source")
	get(fmt.Sprintf("/interim-review-groups/%d", combined.GroupID), &combinedView)
	if combinedView.SourceCheck == nil || combinedView.SourceCheck.State != "blocked" || combinedView.History[0].Outcome.Disposition != "reportable" {
		t.Fatal("invented source was not blocked", combinedView)
	}
	badArchive := store.InterimArchiveRequest{ReviewTaskID: combined.TaskID, ReviewAttempt: 1, ReportSHA256: string(combinedView.History[0].Outcome.ReportSHA256), Title: "Fixtures / Mock fabricated source", Severity: "high"}
	post(fmt.Sprintf("/interim-review-groups/%d/archive", combined.GroupID), badArchive, nil, 409)
	// Replay the two retained title-only shapes through real Go/TS boundaries.
	for i, scenario := range []string{"headline-reportable", "headline-not-applicable"} {
		requestID := fmt.Sprintf("12345678-1234-4123-8123-%012d", 90+i)
		var next created
		post(fmt.Sprintf("/interim-review-groups/%d/retry", combined.GroupID), map[string]any{"requestId": requestID, "acknowledgeUnknownCost": true}, &next, 200)
		run("title-only-"+scenario, scenario)
		get(fmt.Sprintf("/interim-review-groups/%d", combined.GroupID), &combinedView)
		latest := combinedView.History[0]
		if latest.Status != "partial" || latest.Outcome == nil || !strings.Contains(latest.Error, "复核报告输出不完整") || !strings.HasPrefix(latest.Report, "## ") {
			t.Fatal("title-only report was accepted or lost", latest)
		}
		saved, e := s.AttemptResult(t.Context(), next.TaskID, 1)
		if e != nil || saved.Result == nil {
			t.Fatal("partial result lost", e)
		}
		op, e := store.OperationResult(*saved.Result)
		if e != nil || op.Validation == nil || op.Error == nil || op.Error.Code != "incomplete" || op.Cost.State != "unknown" || op.Cost.EstimatedUSD != nil {
			t.Fatal("incomplete result or unknown cost was lost", op, e)
		}
		if string(latest.Outcome.ReportSHA256) != contracts.InterimReviewDigest([]byte(op.Validation.Report)) || latest.InputSHA256 != combinedView.InputSHA256 {
			t.Fatal("original report or frozen evidence binding changed", latest)
		}
		badArchive.ReviewTaskID = next.TaskID
		badArchive.ReportSHA256 = string(latest.Outcome.ReportSHA256)
		post(fmt.Sprintf("/interim-review-groups/%d/archive", combined.GroupID), badArchive, nil, 409)
	}
	// Concurrency uses the real HTTP service; all requests remain Fixture-only.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := doJSON(t, ts.URL+"/api/v1"+fmt.Sprintf("/interim-review-groups/%d/archive", first.GroupID), token, wrong)
			if c != 409 {
				t.Errorf("stale archive accepted: %d", c)
			}
		}()
	}
	wg.Wait()
}
