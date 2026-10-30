package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real Server -> Agent -> Scanner -> synthetic SDK -> loopback Responses.
// No real model endpoint, keys, production data, retries or deployments.
func TestConfiguredBudgetOfflinePipeline(t *testing.T) {
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("fixture-budget", "fixture-password")); e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	if e = os.MkdirAll(filepath.Join(source, "src"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "src/query.ts"), []byte("// Fixtures / Mock\nexport const query = 'fixture';\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-budget.zip", ArchiveSha256: fmt.Sprintf("%x", sha256.Sum256([]byte(scratch))), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var missing atomic.Bool
	var model *store.ModelConfigRow
	rate := func(v string) *contracts.TokenUnitPrice { r := contracts.TokenUnitPrice(v); return &r }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/responses" {
			t.Errorf("unexpected fixture route %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		n := calls.Add(1)
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		format := body["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Error("Responses + Schema contract changed")
		}
		if n == 1 && !missing.Load() {
			copy := *model
			copy.Pricing = &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: rate("100"), OutputPerMillion: rate("100")}
			if e := s.UpdateModelConfig(t.Context(), model.ID, copy); e != nil {
				t.Error(e)
			}
		}
		response := map[string]any{"id": fmt.Sprintf("resp_fixture_%d", n), "status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "Fixtures / Mock"}}}}}
		if !missing.Load() {
			response["usage"] = map[string]any{"input_tokens": 1, "output_tokens": 0, "total_tokens": 1}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer upstream.Close()
	key, e := cryptokey.Encrypt("fixture-loopback-only")
	if e != nil {
		t.Fatal(e)
	}
	model, e = s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock budget", Provider: "deepseek", Model: "deepseek-flash", BaseURL: upstream.URL, WireAPI: "responses", ApiKeyEncrypted: key, Pricing: &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: rate("1"), OutputPerMillion: rate("1")}})
	if e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: "fixture-budget", AgentToken: "fixture-budget-agent", QueueLimits: store.ClaimLimits{Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0}}
	web := httptest.NewServer(server.Routes())
	defer web.Close()
	token := login(t, web.URL, "admin", "fixture-password")
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	for _, scenario := range []string{"token-budget-limit", "token-budget-missing"} {
		t.Setenv("AXIOM_RESULT_TEST_SCENARIO", scenario)
		missing.Store(scenario == "token-budget-missing")
		before := calls.Load()
		task := knowledgeTask(t, web.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxTotalCostUsd": 0.000002})
		runInputAgent(t, agent, root, web.URL, server.AgentToken, filepath.Join(scratch, scenario+".json"))
		current, e := s.Get(t.Context(), task.ID)
		if e != nil || current.Status != "partial" {
			t.Fatal("budget did not preserve partial execution", e, current)
		}
		want := int32(2)
		if missing.Load() {
			want = 1
		}
		if calls.Load()-before != want {
			t.Fatalf("unexpected inference count %d, want %d", calls.Load()-before, want)
		}
		b, e := s.BudgetSnapshot(t.Context(), task.ID)
		if e != nil || len(b.Attempts) != 1 || !b.Attempts[0].TokenEstimate.Present {
			t.Fatal(e, b)
		}
		estimate := b.Attempts[0].TokenEstimate.Value
		if missing.Load() {
			if estimate.Status != "unknown" || estimate.AmountMicros != nil {
				t.Fatal("missing usage became a known fee", estimate)
			}
		} else if estimate.PricingSnapshot.Revision != 1 || estimate.AmountMicros == nil || *estimate.AmountMicros != 2 {
			t.Fatal("running budget used changed prices", estimate)
		}
		if b.RetryAllowed {
			t.Fatal("unknown final fee must not authorize budget retry", b)
		}
		if raw, e := os.ReadFile(filepath.Join(current.OutputPath, "checkpoints/interim/index.json")); e != nil || !strings.Contains(string(raw), "Fixtures / Mock partial candidate") {
			t.Fatal("partial evidence missing", e, string(raw))
		}
	}
	// A queued task must not use a different model's prices after config edits.
	queued := knowledgeTask(t, web.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxTotalCostUsd": 0.000002})
	changed, e := s.GetModelConfig(t.Context(), model.ID)
	if e != nil {
		t.Fatal(e)
	}
	changed.Model = "fixture-renamed-model"
	if e = s.UpdateModelConfig(t.Context(), model.ID, *changed); e != nil {
		t.Fatal(e)
	}
	before := calls.Load()
	runInputAgent(t, agent, root, web.URL, server.AgentToken, filepath.Join(scratch, "price-mismatch.json"))
	blocked, e := s.Get(t.Context(), queued.ID)
	if e != nil || blocked.Status != "failed" || !strings.Contains(blocked.ErrorMessage, "匹配单价") || calls.Load() != before {
		t.Fatal("changed price binding was not stopped before inference", e, blocked)
	}
	archived, e := s.AttemptResult(t.Context(), queued.ID, 1)
	if e != nil || archived.Result == nil {
		t.Fatal("local pricing failure lost its structured result", e, archived)
	}
}
