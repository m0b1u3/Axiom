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

// Exercises frozen old control routes and Scanner descriptions with actual Go
// and Node processes. Only SDK model calls are replaced; historical binaries
// are not claimed to have been certified by this test.
func TestCompatibilityVersionPipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner before compatibility acceptance")
	}
	s := inputPipelineStore(t)
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("Agent build: %v %s", e, out)
	}
	secret, agentToken := "fixture-compatibility-secret", "fixture-compatibility-agent-token"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-not-a-real-model-key")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture compatibility", Provider: "openai", Model: "fixture-compatibility-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	if e = os.MkdirAll(filepath.Join(source, "src"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "src/a,b.ts"), []byte("// Synthetic compatibility fixture; no real audit\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture-compatibility.zip", ArchiveSha256: strings.Repeat("1", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	limits := store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output")}
	srv := &httpserver.Server{Store: s, Secret: secret, AdminUsername: "input-fixture-admin", AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: limits}
	api := srv.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			if _, e := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); e == nil {
				http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer web.Close()
	token := login(t, web.URL, "input-fixture-admin", "fixture-password")
	m, _ := contracts.EncodeJSON(contracts.ResolvedModel{ProviderID: "openai", ConnectionType: "openai", Model: model.Model, WireAPI: "responses", CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "fixture-compatibility"})
	p, _ := contracts.EncodeJSON(contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}})
	var legacyID int64
	for _, c := range []struct {
		name                         string
		server, description, profile int
		want                         string
	}{
		{"old-server-v1", 1, 4, 0, "blocked-policy"},
		{"old-server-v2", 2, 4, 1, "blocked-policy"},
		{"old-server-v3", 3, 4, 2, "blocked-policy"},
		{"legacy-scanner", 4, 1, 0, "completed"},
		{"input-scanner", 4, 2, 1, "completed"},
		{"result-scanner", 4, 3, 2, "completed"},
		{"current-components", 4, 4, 3, "completed"},
		{"input-scanner-new-task", 4, 2, 3, "pending"},
		{"result-scanner-new-task", 4, 3, 3, "pending"},
		{"legacy-scanner-new-task", 4, 1, 3, "pending"},
		{"future-scanner-blocked", 4, 5, 0, "blocked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			profile := control.Profiles()[c.profile]
			id, e := s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-compatibility-" + c.name, SourceSnapshotID: snap.ID, SourceContentHash: snap.ContentHash, SourcePath: source, Provider: model.Provider, Model: model.Model, ModelConfigID: model.ID, ScanMode: "standard", TimeoutMinutes: 1,
				InputContractVersion: profile.Input, ResultContractVersion: profile.Result, ObservationContractVersion: profile.Observation, ModelSnapshotJSON: string(m), PolicySnapshotJSON: string(p), CredentialSnapshotEncrypted: encrypted})
			if e != nil {
				t.Fatal(e)
			}
			old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.server < 4 {
					if strings.HasSuffix(r.URL.Path, "/describe") || strings.HasSuffix(r.URL.Path, "/runtime") {
						w.WriteHeader(404)
						return
					}
					for n := c.server + 1; n <= 4; n++ {
						if strings.HasPrefix(r.URL.Path, fmt.Sprintf("/api/internal/v%d/", n)) {
							w.WriteHeader(404)
							return
						}
					}
				}
				api.ServeHTTP(w, r)
			}))
			defer old.Close()
			file := "scanner-description.json"
			if c.description > 1 {
				file = fmt.Sprintf("scanner-description-v%d.json", min(c.description, 4))
			}
			description, e := os.ReadFile(filepath.Join(root, "contracts/fixtures/v1", file))
			if e != nil {
				t.Fatal(e)
			}
			if c.description == 5 {
				description = []byte(`{"schemaVersion":5}`)
			}
			wrapped := filepath.Join(t.TempDir(), "scanner")
			if e = os.MkdirAll(filepath.Join(wrapped, "dist"), 0700); e != nil {
				t.Fatal(e)
			}
			realMain, _ := json.Marshal(filepath.Join(root, "code-audit-scanner/dist/main.js"))
			frozen, _ := json.Marshal(string(description))
			wrapper := "if (process.argv.includes('--describe')) {process.stdout.write(" + string(frozen) + ");} else {await import(" + string(realMain) + ");}\n"
			if e = os.WriteFile(filepath.Join(wrapped, "package.json"), []byte(`{"type":"module"}`), 0600); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(wrapped, "dist/main.js"), []byte(wrapper), 0600); e != nil {
				t.Fatal(e)
			}
			capture := filepath.Join(scratch, c.name+"-sdk.json")
			ctx, stop := context.WithTimeout(t.Context(), 30*time.Second)
			defer stop()
			cmd := inputAgentCommand(ctx, agent, root, old.URL, agentToken, capture)
			cmd.Env = append(cmd.Env, "AXIOM_SCANNER_DIR="+wrapped)
			out, runErr := cmd.CombinedOutput()
			if (runErr != nil) != (c.want == "blocked" || c.want == "blocked-policy") {
				t.Fatalf("Agent compatibility run: %v %s", runErr, out)
			}
			task, e := s.Get(t.Context(), id)
			if e != nil {
				t.Fatal(e)
			}
			if c.want == "completed" {
				if task.Status != "completed" {
					t.Fatalf("compatible profile failed: %+v %s", task, out)
				}
				if _, e = os.Stat(capture); e != nil {
					t.Fatal("SDK fixture was not reached", e)
				}
				if c.profile == 0 {
					legacyID = id
				}
				return
			}
			if task.Status != "pending" || task.Attempt != 0 {
				t.Fatal("incompatible task acquired an execution", task)
			}
			if _, e = os.Stat(capture); !os.IsNotExist(e) {
				t.Fatal("incompatible combination launched the SDK")
			}
			if c.want == "blocked-policy" {
				if !strings.Contains(string(out), "无配额调度") {
					t.Fatal("missing dispatch policy compatibility error", string(out))
				}
				if code, body := doJSON(t, web.URL+fmt.Sprintf("/api/v1/scans/%d/cancel", id), token, map[string]any{}); code != 200 {
					t.Fatal("Web cancellation", code, body)
				}
				return
			}
			q, e := s.QueueOverview(t.Context(), limits, id)
			if e != nil || len(q.Tasks) != 1 || q.Tasks[0].Compatibility == nil || q.Tasks[0].Compatibility.Reason == "" {
				t.Fatal("missing Web compatibility evidence", q, e)
			}
			if c.want == "blocked" && (q.Agents[0].Runtime == nil || q.Agents[0].Runtime.State != "blocked" || q.Tasks[0].Reason != "scanner_incompatible") {
				t.Fatal("preflight block was not reported", q)
			}
			if code, body := doJSON(t, web.URL+fmt.Sprintf("/api/v1/scans/%d/cancel", id), token, map[string]any{}); code != 200 {
				t.Fatal("Web cancellation", code, body)
			}
		})
	}
	// Legacy attempts, log files, coverage and reports remain readable after new
	// runtime metadata and incompatible component attempts have been recorded.
	for _, suffix := range []string{"", "/attempts", "/attempts/1/result", "/logs?attempt=1", "/coverage", "/report"} {
		code, body := getJSON(t, web.URL+fmt.Sprintf("/api/v1/scans/%d", legacyID)+suffix, token)
		if code != 200 || len(body) == 0 {
			t.Fatal("legacy reader removed", suffix, code)
		}
	}
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		// Keep one genuine pending Web task and an observed preflight block for the
		// browser. Nothing starts an Agent while this UI fixture is inspected.
		code, body := doJSON(t, web.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxCostUsd": 4.25})
		if code != 200 {
			t.Fatal(code, body)
		}
		var created struct {
			Data store.Task `json:"data"`
		}
		if e = json.Unmarshal([]byte(body), &created); e != nil {
			t.Fatal(e)
		}
		if e = s.ReportAgent(t.Context(), "fixture-legacy-node", 1); e != nil {
			t.Fatal(e)
		}
		// Explicitly synthetic old runtime metadata retained across a Server
		// matrix upgrade. This record cannot receive tasks under the new matrix.
		if e = s.ReportAgentWithObservations(t.Context(), "fixture-matrix-node", 1,
			contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true},
			contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}},
			contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}); e != nil {
			t.Fatal(e)
		}
		matrixBuild, matrixDescription := "fixture-old-scanner", int64(4)
		if e = s.ReportAgentRuntime(t.Context(), "fixture-matrix-node", contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "fixture-old-build", ScannerBuild: &matrixBuild, ScannerDescriptionVersion: &matrixDescription, ControlAPI: 4, State: "ready", MatrixHash: contracts.Digest(strings.Repeat("a", 64))}); e != nil {
			t.Fatal(e)
		}
		browser := exec.Command("node", filepath.Join(root, "code-audit-web/tests/compatibility-web-acceptance.mjs"))
		browser.Dir = filepath.Join(root, "code-audit-web")
		browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, "AXIOM_TEST_COMPATIBILITY_ID="+fmt.Sprint(created.Data.ID), "AXIOM_TEST_LEGACY_ID="+fmt.Sprint(legacyID))
		if out, e := browser.CombinedOutput(); e != nil {
			t.Fatalf("compatibility browser: %v\n%s", e, out)
		}
	}
}
