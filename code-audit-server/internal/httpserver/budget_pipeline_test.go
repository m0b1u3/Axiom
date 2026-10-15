package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
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
	"sync/atomic"
	"testing"
	"time"
)

func TestBudgetPipelineSDKAndWeb(t *testing.T) {
	s := inputPipelineStore(t)
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Fatal("build Scanner before budget acceptance")
	}
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("Agent build %v %s", e, out)
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	secret, agentToken := "fixture-budget-secret", "fixture-budget-agent-token"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-budget-not-a-real-key")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture budget SDK (no model calls)", Provider: "openai", Model: "fixture-budget-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	if e = os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Synthetic budget fixture, not real audit source\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture-budget.zip", ArchiveSha256: strings.Repeat("2", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, AdminUsername: "input-fixture-admin", QueueLimits: store.ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, MaxRecoveries: 2, OutputRoot: filepath.Join(scratch, "output")}}
	handler := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			handler.ServeHTTP(w, r)
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
	create := func(total float64) int64 {
		code, body := doJSON(t, web.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxCostUsd": 4.25, "maxTotalCostUsd": total})
		if code != 200 {
			t.Fatalf("create %d %s", code, body)
		}
		var reply struct {
			Data store.Task `json:"data"`
		}
		json.Unmarshal([]byte(body), &reply)
		return reply.Data.ID
	}
	for _, total := range []float64{0, -1, 0.0000001, 1.1234567} {
		code, _ := doJSON(t, web.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxTotalCostUsd": total})
		if code != 400 {
			t.Fatal("invalid total", total, code)
		}
	}
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
	id := create(2.25)
	var completed atomic.Int32
	// Lose the first ACK after the complete result and cost transaction committed.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") && completed.Add(1) == 1 {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Errorf("first completion %d %s", recorder.Code, recorder.Body.String())
			}
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	for index, want := range []float64{2.25, 1.125} {
		if index > 0 {
			code, body := doJSON(t, web.URL+"/api/v1/scans/"+strconv.FormatInt(id, 10)+"/retry", token, map[string]any{})
			if code != 200 {
				t.Fatalf("retry %d %s", code, body)
			}
		}
		capture := filepath.Join(scratch, fmt.Sprint("budget-run-", index))
		runInputAgent(t, agent, root, proxy.URL, agentToken, capture)
		assertSDKCallCount(t, capture, 1)
		raw, e := os.ReadFile(capture)
		if e != nil {
			t.Fatal(e)
		}
		var sdk struct {
			FixtureOnly bool           `json:"fixtureOnly"`
			Options     map[string]any `json:"options"`
			Leaked      bool           `json:"leakedServerCredentials"`
		}
		json.Unmarshal(raw, &sdk)
		if !sdk.FixtureOnly || sdk.Leaked || sdk.Options["maxCostUsd"] != want {
			t.Fatalf("SDK allowance %v, want %v", sdk.Options, want)
		}
		task, e := s.Get(t.Context(), id)
		if e != nil || task.Status != "completed" || task.Attempt != index+1 || *task.MaxCostUSD != 4.25 {
			t.Fatal(task, e)
		}
	}
	budget, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || *budget.EstimatedMicros != 2250000 || budget.State != "exhausted" || len(budget.Attempts) != 2 {
		t.Fatal(budget, e)
	}
	code, _ := doJSON(t, web.URL+"/api/v1/scans/"+strconv.FormatInt(id, 10)+"/retry", token, map[string]any{})
	if code != 409 {
		t.Fatal("spent budget retry", code)
	}
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "empty")
	unknown := create(5)
	runInputAgent(t, agent, root, web.URL, agentToken, filepath.Join(scratch, "budget-unknown"))
	budget, e = s.BudgetSnapshot(t.Context(), unknown)
	if e != nil || budget.State != "unknown" || budget.RemainingMicros != nil {
		t.Fatal(budget, e)
	}
	code, _ = doJSON(t, web.URL+"/api/v1/scans/"+strconv.FormatInt(unknown, 10)+"/retry", token, map[string]any{})
	if code != 409 {
		t.Fatal("unknown became zero", code)
	}
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "failed")
	partialCost := create(5)
	runInputAgent(t, agent, root, web.URL, agentToken, filepath.Join(scratch, "budget-partial-cost"))
	budget, e = s.BudgetSnapshot(t.Context(), partialCost)
	if e != nil || budget.State != "unknown" || budget.RemainingMicros != nil || *budget.EstimatedMicros != 1125000 || budget.Attempts[0].FinalMicros != nil || budget.Attempts[0].Source != "sdk_progress" {
		t.Fatal("callback cost became a final settlement", budget, e)
	}
	runBudgetProcessFault(t, s, server, agent, root, web.URL, agentToken, scratch, create)
	// Keep an explicit old task to prove the new cost API does not reinterpret it.
	legacy, e := s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-budget-history", SourcePath: source, Provider: "openai", Model: model.Model})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestCancel(t.Context(), legacy); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		cmd := exec.Command("node", "tests/budget-web-acceptance.mjs")
		cmd.Dir = filepath.Join(root, "code-audit-web")
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, "AXIOM_TEST_BUDGET_ID="+strconv.FormatInt(id, 10), "AXIOM_TEST_UNKNOWN_BUDGET_ID="+strconv.FormatInt(unknown, 10), "AXIOM_TEST_BUDGET_LEGACY_ID="+strconv.FormatInt(legacy, 10), "AXIOM_TEST_BUDGET_MODEL_ID="+strconv.FormatInt(model.ID, 10), "AXIOM_TEST_BUDGET_SNAPSHOT_ID="+strconv.FormatInt(snap.ID, 10))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("budget browser %v %s", e, out)
		} else {
			t.Log(string(out))
		}
	}
	if raw, e := contracts.EncodeJSON(budget); e != nil || contracts.ValidateJSON(contracts.KindTaskBudgetSnapshot, raw, nil) != nil {
		t.Fatal("portable budget snapshot", e)
	}
}
