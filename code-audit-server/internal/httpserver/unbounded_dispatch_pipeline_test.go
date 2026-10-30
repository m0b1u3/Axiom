package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/sourceidentity"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Real Server, Agent and Scanner; only the SDK inference boundary is substituted.
// Every database write is to the explicitly isolated input test database.
func TestUnboundedDispatchPipeline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux release process-shutdown acceptance")
	}
	s := inputPipelineStore(t)
	ctx := t.Context()
	root, _ := filepath.Abs("../../..")
	scratch := t.TempDir()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	secret, agentToken := "fixture-unbounded-secret", "fixture-unbounded-agent-token"
	admin, err := s.EnsureAdministrator(ctx, "input-fixture-admin", auth.HashPassword(secret, "fixture-password"))
	if err != nil {
		t.Fatal(err)
	}
	limits := store.ClaimLimits{Global: 1, PerModel: 1, PerProject: 1, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output")}
	settings, err := s.QueueSettings(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveQueueSettings(ctx, settings, "fixture-legacy-history"); err != nil {
		t.Fatal(err)
	}
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
	project, err := s.CreateProject(ctx, "Fixtures / Mock unrestricted dispatch", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cryptokey.Encrypt("fixture-unbounded-key-not-real")
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelConfig(ctx, store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Fixtures / Mock unrestricted model", Provider: "openai", Model: "fixture-unbounded-model", ApiKeyEncrypted: encrypted})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(scratch, "source")
	os.MkdirAll(source, 0700)
	os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Fixtures / Mock only\n"), 0600)
	hash, count, size, err := sourceidentity.TreeHash(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.InsertSnapshot(ctx, store.SnapshotRow{OriginalFilename: "Fixtures-Mock-unbounded.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size, ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	const tasks = 7
	ids := []int64{}
	for i := 0; i < tasks; i++ {
		status, raw := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snapshot.ID, "modelConfigId": model.ID, "maxCostUsd": 3, "maxTotalCostUsd": 10})
		if status != 200 {
			t.Fatal(status, raw)
		}
		var result struct{ Data store.Task }
		if err = json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, result.Data.ID)
	}
	// The obsolete mutation endpoint cannot reactivate the retired policy.
	request, _ := http.NewRequest("PUT", ts.URL+"/api/v1/settings/execution", strings.NewReader(`{"global":1,"perModel":1,"perProject":1,"revision":2}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatal("retired API still writable", response.StatusCode)
	}
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if raw, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(raw))
	}
	capture := filepath.Join(scratch, "sdk-fixture.json")
	os.WriteFile(capture+".hold", []byte("Fixtures / Mock"), 0600)
	commandCtx, kill := context.WithCancel(ctx)
	defer kill()
	command := exec.CommandContext(commandCtx, agent)
	command.Dir = filepath.Join(root, "code-audit-agent")
	command.Env = append(os.Environ(), "AXIOM_SERVER_URL="+ts.URL, "AXIOM_AGENT_TOKEN="+agentToken, "AXIOM_AGENT_ID=fixture-unbounded-agent", "AXIOM_AGENT_CONCURRENCY=1", "AXIOM_POLL_INTERVAL=50ms", "AXIOM_DATA_DIR="+filepath.Join(scratch, "agent-data"), "AXIOM_SCANNER_DIR="+filepath.Join(root, "code-audit-scanner"), "AXIOM_SCAN_MOCK=0", "AXIOM_INPUT_TEST_CAPTURE="+capture, "AXIOM_RESULT_TEST_SCENARIO=reliability-controlled", "NODE_OPTIONS=--import="+filepath.Join(root, "contracts/tests/input-sdk-register.mjs"))
	log, err := os.Create(filepath.Join(scratch, "agent.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			command.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				kill()
				<-done
			}
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		raw, _ := os.ReadFile(capture + ".calls")
		if len(strings.Fields(string(raw))) == tasks {
			break
		}
		if time.Now().After(deadline) {
			text, _ := os.ReadFile(filepath.Join(scratch, "agent.log"))
			t.Fatal("Agent retained fixed slots or failed before fixture SDK entry", string(text))
		}
		time.Sleep(50 * time.Millisecond)
	}
	overview, err := s.QueueOverview(ctx, limits, 0)
	if err != nil || overview.Active != tasks || overview.EffectiveCapacity != nil || overview.DispatchMode != "unbounded" || len(overview.Agents) != 1 || overview.Agents[0].Slots != 0 || overview.Agents[0].DispatchMode != "unbounded" {
		t.Fatal("unrestricted execution not observed", err, overview)
	}
	if os.Getenv("AXIOM_TEST_WEB_DISPATCH") == "1" {
		browser := exec.Command("node", filepath.Join(root, "code-audit-web/tests/unbounded-dispatch-web-acceptance.mjs"))
		browser.Dir = root
		browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL)
		if raw, e := browser.CombinedOutput(); e != nil {
			t.Fatal("dispatch browser", e, string(raw))
		} else {
			t.Log(string(raw))
		}
	}
	if err = command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		stopped = true
		if err != nil {
			raw, _ := os.ReadFile(filepath.Join(scratch, "agent.log"))
			t.Fatal(err, string(raw))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Agent did not join interrupted executions")
	}
	// Stopping the service is an interruption, not a user cancellation. The
	// existing lease-expiry path must retain unknown cost and prevent replay.
	db, err := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE project_id=$1`, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE scan_attempts a SET lease_expires_at='2000-01-01T00:00:00Z' FROM scan_tasks t WHERE t.id=a.task_id AND t.project_id=$1`, project.ID); err != nil {
		t.Fatal(err)
	}
	if replay, e := s.ClaimExecution(ctx, "fixture-after-stop", limits); e != nil || replay != nil {
		t.Fatal("interrupted budgeted work was replayed", e, replay)
	}
	for _, id := range ids {
		task, e := s.Get(ctx, id)
		if e != nil || task.Status != "failed" || task.Attempt != 1 {
			t.Fatal("interrupted work still active or replayed", id, e, task)
		}
		budget, e := s.BudgetSnapshot(ctx, id)
		if e != nil || len(budget.UnknownAttempts) != 1 {
			t.Fatal("interrupted SDK cost was discarded", id, e, budget)
		}
	}
	t.Log(fmt.Sprintf("Fixtures / Mock: %d simultaneous SDK entries despite legacy global/model/project=1 and AXIOM_AGENT_CONCURRENCY=1; zero real model calls", tasks))
}
