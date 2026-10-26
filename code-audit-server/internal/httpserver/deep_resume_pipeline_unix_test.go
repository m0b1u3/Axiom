//go:build !windows

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
	"encoding/base64"
	"encoding/json"
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

// Native SDK registration/inspection is real. Only model execution is replaced.
func TestDeepResumePipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner first")
	}
	s := inputPipelineStore(t)
	scratch, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	agent := filepath.Join(scratch, "agent")
	cmd := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	cmd.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	secret, agentToken := "fixture-deep-session-secret", "fixture-deep-agent-token"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	limits := store.ClaimLimits{Lease: time.Minute, OutputRoot: filepath.Join(scratch, "scans"), MaxRecoveries: 0, SDKDeepResumeCapable: true, SDKConcurrencyCapable: true}
	srv := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: limits}
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
	source := filepath.Join(scratch, "source")
	if e = os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-deep.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-only-no-real-key")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock Deep", Provider: "openai", Model: "gpt-6-sol", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	caps := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	results := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	observations := contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	node := "fixture-input-agent"
	version := int64(14)
	build := "fixture-deep-scanner"
	report := func() {
		t.Helper()
		if e = s.ReportAgentWithObservations(t.Context(), node, 1, caps, results, observations); e != nil {
			t.Fatal(e)
		}
		if e = s.ReportAgentRuntime(t.Context(), node, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixture", ScannerBuild: &build, ControlAPI: 4, State: "ready", MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}); e != nil {
			t.Fatal(e)
		}
	}
	ids := map[string]int64{}
	original := map[string]any{}
	for _, name := range []string{"ready", "missing", "standard"} {
		mode := "deep"
		if name == "standard" {
			mode = "standard"
		}
		body := map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "scanMode": mode}
		if name == "ready" {
			body["maxCostUsd"] = 2
			body["maxTotalCostUsd"] = 3
		}
		created := knowledgeTask(t, ts.URL, token, body)
		report()
		task, e := s.ClaimExecutionWithObservations(t.Context(), node, limits, caps, results, observations)
		if e != nil || task == nil || task.ID != created.ID {
			t.Fatal("initial fixture claim", task, e)
		}
		ids[name] = task.ID
		if name == "ready" {
			manifest := map[string]any{"fixtureOnly": true, "outputPath": task.OutputPath, "sourcePath": source, "sourceHash": hash, "configurationHash": store.DeepConfigurationHash(task), "model": model.Model, "execution": contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}, "agentId": node}
			input := filepath.Join(scratch, "origin-input.json")
			raw, _ := json.Marshal(manifest)
			if e = os.WriteFile(input, raw, 0600); e != nil {
				t.Fatal(e)
			}
			setup := exec.Command("node", "contracts/tests/fixtures/deep-resume-origin.mjs", input)
			setup.Dir = root
			if out, e := setup.CombinedOutput(); e != nil {
				t.Fatal("native fixture", e, string(out))
			} else {
				for _, line := range strings.Split(string(out), "\n") {
					if json.Unmarshal([]byte(line), &original) == nil && original["fixtureOnly"] == true {
						break
					}
				}
			}
		}
		code := int64(1)
		if e = s.CompleteContractExecution(t.Context(), contracts.ExecutionCompletion{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}, AgentID: contracts.Identifier(node), ExecutionStatus: "failed", Process: contracts.ProcessExit{Code: &code, ResultCommitted: false}, Error: contracts.Some(contracts.ContractError{Code: "unknown", Origin: "agent", Message: "Fixtures / Mock hard crash before SDK finalization", Retryable: false})}); e != nil {
			t.Fatal(e)
		}
	}
	if code, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "deep-fixture-viewer", "password": "fixture-password", "role": "viewer"}); code != 200 {
		t.Fatal(code, body)
	}
	if os.Getenv("AXIOM_TEST_WEB_DEEP_RESUME") == "1" {
		report()
		raw, _ := json.Marshal(ids)
		native, _ := json.Marshal(original)
		browser := exec.Command("node", "tests/deep-resume-web-acceptance.mjs")
		browser.Dir = filepath.Join(root, "code-audit-web")
		browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_DEEP_IDS="+string(raw), "AXIOM_TEST_DEEP_ORIGINAL="+string(native), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
		if out, e := browser.CombinedOutput(); e != nil {
			t.Fatal("Deep browser", e, string(out))
		} else {
			t.Log(string(out))
		}
	} else {
		id := ids["ready"]
		task, _ := s.Get(t.Context(), id)
		ref := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}
		if code, body := doJSON(t, ts.URL+fmt.Sprintf("/api/v1/scans/%d/deep-resume/check", id), token, ref); code != 200 {
			t.Fatal(code, body)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		probe := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, filepath.Join(scratch, "probe.json"))
		probe.Env = append(probe.Env, "AXIOM_DEEP_RESUME_FIXTURE=1")
		if out, e := probe.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
		view, e := s.DeepResumeSnapshot(t.Context(), id)
		if e != nil || view.Inspection == nil || !view.Inspection.Eligible || view.CanResume || view.State != "blocked" {
			t.Fatal("unknown fees bypassed", view, e)
		}
		if view.Execution.Attempt != int64(task.Attempt) {
			t.Fatal(view)
		}
	}
}
