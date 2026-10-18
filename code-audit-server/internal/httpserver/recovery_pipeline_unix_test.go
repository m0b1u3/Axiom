//go:build !windows

package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The production Agent and Scanner run, but a module loader substitutes every
// SDK model call. No synthetic recovery path is compiled into production.
func TestRecoveryPipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner before recovery acceptance")
	}
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build Agent: %v %s", e, out)
	}
	secret, agentToken := "fixture-recovery-session-secret", "fixture-recovery-agent-token"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0}}
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
	if e = os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Fixtures / Mock: recovery acceptance only\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "recovery-fixture.zip", ArchiveSha256: fmt.Sprintf("%x", sha256.Sum256([]byte(scratch))), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-only-not-a-real-key")
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture recovery model", Provider: "openai", Model: "fixture-recovery-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ids := map[string]int64{}
	captures := map[string]string{}
	create := func(name string) *store.Task {
		t.Helper()
		task := knowledgeTask(t, ts.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": cfg.ID, "maxCostUsd": 2, "maxTotalCostUsd": 2.5})
		ids[name] = task.ID
		return task
	}
	expire := func(id int64) {
		t.Helper()
		if _, e = db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, id); e != nil {
			t.Fatal(e)
		}
	}
	// A completed normalized result exists, but the Agent dies before the
	// completion can reach Server. Preserve this for the browser to restore.
	for _, name := range []string{"committed", "corrupt"} {
		t.Run("agent-dies-after-commit-"+name, func(t *testing.T) {
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
			task := create(name)
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/complete") {
					select {
					case entered <- struct{}{}:
					default:
					}
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				api.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			defer close(release)
			capture := filepath.Join(scratch, name+".json")
			captures[name] = capture
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := inputAgentCommand(ctx, agent, root, proxy.URL, agentToken, capture)
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer cmd.Process.Kill()
			select {
			case <-entered:
			case <-time.After(12 * time.Second):
				t.Fatal("fixture never reached committed completion")
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Agent kill did not finish")
			}
			expire(task.ID)
			assertSDKCallCount(t, capture, 1)
			current, _ := s.Get(t.Context(), task.ID)
			if name == "corrupt" {
				if e = os.WriteFile(filepath.Join(current.OutputPath, "normalized/result.json"), []byte("{\"truncated\":"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			// New Store process state reads the same durable DB and filesystem.
			reopened, e := store.Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			state, e := reopened.RecoverySnapshot(t.Context(), task.ID, srv.QueueLimits.OutputRoot)
			reopened.Close()
			want := "recoverable"
			if name == "corrupt" {
				want = "invalid"
			}
			if e != nil || string(state.CommittedResult.State) != want {
				t.Fatal(state, e)
			}
			if state.CheckpointResume.Supported {
				t.Fatal("restart mislabeled as resume")
			}
		})
	}
	t.Run("agent-dies-before-result-commit", func(t *testing.T) {
		t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "reliability-hold")
		task := create("missing")
		capture := filepath.Join(scratch, "missing.json")
		captures["missing"] = capture
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		defer cmd.Process.Kill()
		var processes struct {
			ScannerPID int `json:"scannerPid"`
			ChildPID   int `json:"childPid"`
		}
		waitReliability(t, 8*time.Second, func() bool {
			raw, e := os.ReadFile(capture + ".processes")
			return e == nil && json.Unmarshal(raw, &processes) == nil && processes.ScannerPID > 1 && processes.ChildPID > 1
		}, "fixture did not begin")
		defer syscall.Kill(-processes.ScannerPID, syscall.SIGKILL)
		defer syscall.Kill(processes.ChildPID, syscall.SIGKILL)
		if e = cmd.Process.Kill(); e != nil {
			t.Fatal(e)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Agent kill did not finish")
		}
		waitReliability(t, 5*time.Second, func() bool {
			return syscall.Kill(processes.ScannerPID, 0) != nil && syscall.Kill(processes.ChildPID, 0) != nil
		}, "orphan Scanner or SDK child survived")
		expire(task.ID)
		assertSDKCallCount(t, capture, 1)
		state, e := s.RecoverySnapshot(t.Context(), task.ID, srv.QueueLimits.OutputRoot)
		if e != nil || state.CommittedResult.State != "missing" || !state.Interruption.Allowed {
			t.Fatal(state, e)
		}
	})
	legacy, e := s.InsertPending(t.Context(), store.Task{ScanNo: "Fixture legacy recovery", SourcePath: source, Provider: "openai", Model: "fixture-legacy-model", ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestCancel(t.Context(), legacy); e != nil {
		t.Fatal(e)
	}
	ids["legacy"] = legacy
	t.Run("http-contract-and-permissions", func(t *testing.T) {
		id := ids["committed"]
		url := ts.URL + fmt.Sprintf("/api/v1/scans/%d/recovery", id)
		for _, body := range []map[string]any{
			{"schemaVersion": 1, "execution": map[string]any{"taskId": fmt.Sprint(id), "attempt": 1}, "action": "resume", "expectedStatus": "scanning", "resultSha256": nil, "acknowledgeReexecution": false},
			{"schemaVersion": 1, "execution": map[string]any{"taskId": fmt.Sprint(id), "attempt": 1}, "action": "restart", "expectedStatus": "failed", "resultSha256": nil, "acknowledgeReexecution": false},
		} {
			if code, _ := doJSON(t, url, token, body); code != 400 {
				t.Fatal("invalid recovery action accepted", code)
			}
		}
		if code, _ := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "recovery-fixture-viewer", "password": "fixture-password", "role": "viewer"}); code != 200 {
			t.Fatal(code)
		}
		viewer := login(t, ts.URL, "recovery-fixture-viewer", "fixture-password")
		if code, _ := doJSON(t, url, viewer, map[string]any{}); code != 403 {
			t.Fatal("viewer gained recovery write", code)
		}
		state, e := s.RecoverySnapshot(t.Context(), id, srv.QueueLimits.OutputRoot)
		if e != nil {
			t.Fatal(e)
		}
		action := contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: state.Execution, ExpectedStatus: contracts.ScanRecoveryActionExpectedStatus(state.TaskStatus), Action: "restore_result", ResultSHA256: state.CommittedResult.SHA256}
		action.Execution.TaskID = contracts.TaskID(fmt.Sprint(ids["corrupt"]))
		if code, _ := doJSON(t, url, token, action); code != 400 {
			t.Fatal("cross-task recovery accepted", code)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_RECOVERY") == "1" {
		t.Run("browser-recovery", func(t *testing.T) {
			data, _ := json.Marshal(ids)
			cmd := exec.Command("node", "tests/scan-recovery-web-acceptance.mjs")
			cmd.Dir = filepath.Join(root, "code-audit-web")
			cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_RECOVERY_IDS="+string(data), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("recovery browser: %v %s", e, out)
			} else {
				t.Log(string(out))
			}
		})
	} else {
		state, _ := s.RecoverySnapshot(t.Context(), ids["committed"], srv.QueueLimits.OutputRoot)
		action := contracts.ScanRecoveryAction{SchemaVersion: 1, Execution: state.Execution, ExpectedStatus: contracts.ScanRecoveryActionExpectedStatus(state.TaskStatus), Action: "restore_result", ResultSHA256: state.CommittedResult.SHA256}
		if code, body := doJSON(t, ts.URL+fmt.Sprintf("/api/v1/scans/%d/recovery", ids["committed"]), token, action); code != 200 {
			t.Fatal(code, body)
		}
	}
	assertSDKCallCount(t, captures["committed"], 1)
	assertSDKCallCount(t, captures["corrupt"], 1)
	assertSDKCallCount(t, captures["missing"], 1)
}
