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
	"testing"
	"time"
)

// Full application/Agent/Scanner pipeline. The SDK module is replaced by the
// repository's offline fixture; its upstream address is also loopback-only.
func TestModelPricingOfflinePipeline(t *testing.T) {
	t.Setenv("AXIOM_SDK050_FIXTURE", "1")
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
	t.Setenv("AXIOM_VALIDATION_OUTPUT_MODE", "native_schema")
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword("fixture-pricing-session", "fixture-password")); e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "Fixtures-Mock-source")
	if e = os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Fixtures / Mock pricing acceptance only. No service or credentials.\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-pricing.zip", ArchiveSha256: fmt.Sprintf("%x", sha256.Sum256([]byte(scratch))), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-only-not-a-model-key")
	if e != nil {
		t.Fatal(e)
	}
	rate := func(v string) *contracts.TokenUnitPrice { n := contracts.TokenUnitPrice(v); return &n }
	price := &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "cache_read", InputPerMillion: rate("1"), CacheReadPerMillion: rate("0.1"), OutputPerMillion: rate("4"), Note: "Fixtures / Mock price; no real model requests"}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "DeepSeek · Fixtures / Mock", Provider: "deepseek", Model: "deepseek-flash", BaseURL: "http://127.0.0.1:1", WireAPI: "responses", ApiKeyEncrypted: encrypted, Pricing: price})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetModelConfigEnabled(t.Context(), model.ID, true); e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: "fixture-pricing-session", AgentToken: "fixture-pricing-agent", SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0, SDKConcurrencyCapable: true}}
	routes := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer web.Close()
	token := login(t, web.URL, "input-fixture-admin", "fixture-password")
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	verify := func(id int64, micros int64, revision int64) {
		t.Helper()
		b, e := s.BudgetSnapshot(t.Context(), id)
		if e != nil || len(b.Attempts) != 1 || !b.Attempts[0].TokenEstimate.Present {
			t.Fatal(e, b)
		}
		estimate := b.Attempts[0].TokenEstimate.Value
		if estimate.Status != "estimated" || estimate.Usage.InputTokens == nil || *estimate.Usage.InputTokens != 123 || estimate.PricingSnapshot.Revision != revision || estimate.AmountMicros == nil || *estimate.AmountMicros != micros || b.Attempts[0].Source != "configured_pricing" {
			t.Fatal("wrong pipeline estimate", estimate, b)
		}
		result, e := s.AttemptResult(t.Context(), id, 1)
		if e != nil {
			t.Fatal(e)
		}
		if result.Result.Cost.Present && result.Result.Cost.Value != nil && result.Result.Cost.Value.EstimatedUSD != nil {
			t.Fatal("SDK unknown cost was rewritten", result.Result.Cost.Value)
		}
	}
	first := knowledgeTask(t, web.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "maxTotalCostUsd": 1})
	runInputAgent(t, agent, root, web.URL, server.AgentToken, filepath.Join(scratch, "first-capture.json"))
	verify(first.ID, 105, 1)
	price.InputPerMillion = rate("2")
	price.CacheReadPerMillion = rate("0.2")
	price.OutputPerMillion = rate("8")
	model.Pricing = price
	model.PricingSet = true
	if e = s.UpdateModelConfig(t.Context(), model.ID, *model); e != nil {
		t.Fatal(e)
	}
	second := knowledgeTask(t, web.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID})
	runInputAgent(t, agent, root, web.URL, server.AgentToken, filepath.Join(scratch, "second-capture.json"))
	verify(first.ID, 105, 1)
	verify(second.ID, 210, 2)
	findings, e := s.ListFindings(t.Context(), first.ID)
	if e != nil || len(findings) == 0 {
		t.Fatal(e, findings)
	}
	code, raw := doJSON(t, web.URL+fmt.Sprintf("/api/v1/scans/%d/sdk-operations", first.ID), token, map[string]any{"kind": "validate", "findingId": fmt.Sprint(findings[0].ID), "originAttempt": 1, "acknowledgeUnknownCost": true})
	if code != 200 {
		t.Fatal(code, raw)
	}
	var created struct{ Data store.Task }
	if e = json.Unmarshal([]byte(raw), &created); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AXIOM_OPERATION_TEST_SCENARIO", "reportable")
	runInputAgent(t, agent, root, web.URL, server.AgentToken, filepath.Join(scratch, "validation-capture.json"))
	b, e := s.BudgetSnapshot(t.Context(), created.Data.ID)
	if e != nil || len(b.Attempts) != 1 || !b.Attempts[0].TokenEstimate.Present {
		t.Fatal(e, b)
	}
	unknown := b.Attempts[0].TokenEstimate.Value
	if unknown.Status != "unknown" || unknown.AmountMicros != nil || unknown.Usage != nil || b.Attempts[0].State != "unknown" {
		t.Fatal("validation fabricated usage", unknown, b)
	}
	mode, e := os.ReadFile(filepath.Join(scratch, "output", fmt.Sprint(created.Data.ID), "attempt-1/logs/validation-output-mode.json"))
	if e != nil || !strings.Contains(string(mode), "native_schema") {
		t.Fatal(e, string(mode))
	}
	if report := os.Getenv("AXIOM_PRICING_UI_REPORT"); report != "" {
		ready := map[string]any{"fixtureOnly": true, "modelCalls": 0, "baseUrl": web.URL, "modelConfigId": model.ID, "firstTaskId": first.ID, "secondTaskId": second.ID, "unknownReviewTaskId": created.Data.ID, "username": "input-fixture-admin", "password": "fixture-password", "releasePath": report + ".release"}
		bytes, _ := json.MarshalIndent(ready, "", "  ")
		if e = os.WriteFile(report, bytes, 0600); e != nil {
			t.Fatal(e)
		}
		deadline := time.Now().Add(12 * time.Minute)
		for {
			if _, e = os.Stat(report + ".release"); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("UI acceptance was not released")
			}
			select {
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
}
