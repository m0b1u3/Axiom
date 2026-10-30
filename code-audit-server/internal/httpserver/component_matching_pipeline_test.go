package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// All inference boundaries are fixtures; both upstreams and the app bind only
// loopback. The real API, Agent, pinned component scheduler and fee store run.
func TestComponentMatchingUsagePipeline(t *testing.T) {
	s := inputPipelineStore(t)
	root, _ := filepath.Abs("../../..")
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if output, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(output))
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("AXIOM_COMPONENT_SCAN_FIXTURE", "1")
	t.Setenv("AXIOM_COMPONENT_USAGE_FIXTURE", "1")
	t.Setenv("AXIOM_COMPONENT_SCAN_SCENARIO", "normal")
	secret, agentToken := "fixture-matching-session", "fixture-matching-agent"
	if _, e := s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Global: 3, PerModel: 3, PerProject: 3, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 3, TokenBudgetCapable: true, SDKConcurrencyCapable: true, SDKParametersCapable: true}}
	routes := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
		} else if _, e := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); e == nil && r.URL.Path != "/" {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
		} else {
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
		}
	}))
	defer app.Close()
	token := login(t, app.URL, "input-fixture-admin", "fixture-password")
	key, _ := cryptokey.Encrypt("fixture-matching-key-not-real")
	if _, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock normalization", Provider: "openai", Model: "fixture-local-only", ApiKeyEncrypted: key, Pricing: inputFixturePrice()}); e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(scratch, "source")
	for _, name := range []string{"api/a.ts", "web/b.ts", "worker/c.ts"} {
		file := filepath.Join(source, name)
		os.MkdirAll(filepath.Dir(file), 0700)
		os.WriteFile(file, []byte("// Fixtures / Mock only\n"), 0600)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-matching.zip", ArchiveSha256: strings.Repeat("b", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	var document contracts.ComponentPlanDocument
	json.Unmarshal([]byte(`{"components":[{"name":"Fixtures / Mock API","paths":["api"]},{"name":"Fixtures / Mock Web","paths":["web"]},{"name":"Fixtures / Mock Worker","paths":["worker"]}]}`), &document)
	plan, e := s.CreateComponentPlan(t.Context(), snapshot.ID, "Fixtures / Mock matching usage", document, 1)
	if e != nil {
		t.Fatal(e)
	}
	code, raw := doJSON(t, app.URL+"/api/v1/component-plans/"+string(plan.ID)+"/operations", token, map[string]any{"revision": 1, "kind": "normalize"})
	var normal struct{ Data store.Task }
	if code != 200 || json.Unmarshal([]byte(raw), &normal) != nil {
		t.Fatal(code, raw)
	}
	runInputAgent(t, agent, root, app.URL, agentToken, filepath.Join(scratch, "normalize"))
	normalized, status, e := s.ComponentOperationResult(t.Context(), normal.Data.ID)
	if e != nil || status != "completed" || normalized == nil {
		t.Fatal(e, status)
	}
	version, e := s.FreezeComponentPlan(t.Context(), int64From(plan.ID), 1, normal.Data.ID, 1, store.ComponentResultDigest(normalized))
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]int64{}
	var historicalBudget contracts.TaskBudgetSnapshot
	for _, scenario := range []string{"complete", "missing-usage", "partial-usage", "http-failure", "change-price"} {
		t.Run(scenario, func(t *testing.T) {
			var matchingCalls, allCalls atomic.Int64
			var cfg *store.ModelConfigRow
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string            `json:"model"`
					Metadata map[string]string `json:"metadata"`
					Text     struct {
						Format struct {
							Type   string
							Strict bool
						}
					} `json:"text"`
				}
				if e := json.NewDecoder(r.Body).Decode(&request); e != nil || r.URL.Path != "/responses" || request.Model != "deepseek-flash" || request.Text.Format.Type != "json_schema" || !request.Text.Format.Strict {
					t.Error("loopback Responses + Schema fixture changed", e)
					http.Error(w, "Fixtures / Mock invalid request", 400)
					return
				}
				n := allCalls.Add(1)
				matching := request.Metadata["stage"] == "matching"
				m := int64(0)
				if matching {
					m = matchingCalls.Add(1)
				}
				if matching && scenario == "http-failure" && m == 2 {
					http.Error(w, `{"error":{"message":"Fixtures / Mock matching failure"}}`, 503)
					return
				}
				input, cached, output := int64(20), int64(8), int64(5)
				if matching {
					input, cached, output = 100, 60, 10
				}
				usage := map[string]any{"input_tokens": input, "input_tokens_details": map[string]any{"cached_tokens": cached}, "output_tokens": output, "output_tokens_details": map[string]any{"reasoning_tokens": 2}, "total_tokens": input + output}
				response := map[string]any{"id": fmt.Sprintf("fixture-matching-%d", n), "status": "completed", "usage": usage, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": `{"matches":[],"uncertain":[],"related":[],"request":null}`}}}}}
				if matching && (scenario == "missing-usage" || scenario == "partial-usage" && m == 2) {
					delete(response, "usage")
				}
				if matching && scenario == "change-price" && m == 1 {
					changed := *cfg
					pricing := *cfg.Pricing
					rate := contracts.TokenUnitPrice("99")
					pricing.InputPerMillion = &rate
					changed.Pricing, changed.PricingSet = &pricing, true
					if e := s.UpdateModelConfig(t.Context(), cfg.ID, changed); e != nil {
						t.Error(e)
					}
				}
				data, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", data)
			}))
			defer upstream.Close()
			input, cached, output := contracts.TokenUnitPrice("0.3"), contracts.TokenUnitPrice("0.006"), contracts.TokenUnitPrice("1.2")
			cfg, e = s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock matching " + scenario, Provider: "deepseek", BaseURL: upstream.URL, Model: "deepseek-flash", WireAPI: "responses", ApiKeyEncrypted: key, Pricing: &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "cache_read", InputPerMillion: &input, CacheReadPerMillion: &cached, OutputPerMillion: &output}})
			if e != nil {
				t.Fatal(e)
			}
			code, raw := doJSON(t, app.URL+"/api/v1/component-plans/"+string(plan.ID)+"/scans", token, map[string]any{"version": version.Version, "versionSha256": version.SHA256, "modelConfigId": cfg.ID, "workers": 1, "timeoutMinutes": 2, "componentCostUsd": 2, "maxCostUsd": 6, "maxTotalCostUsd": 6, "acknowledgeMatchingCost": true})
			var created struct{ Data struct{ ID int64 } }
			if code != 200 || json.Unmarshal([]byte(raw), &created) != nil {
				t.Fatal(code, raw)
			}
			id := created.Data.ID
			ids[scenario] = id
			runInputAgent(t, agent, root, app.URL, agentToken, filepath.Join(scratch, "capture-"+scenario))
			task, e := s.Get(t.Context(), id)
			if e != nil || task.Status != map[bool]string{true: "partial", false: "completed"}[scenario == "http-failure"] {
				t.Fatal(e, task)
			}
			archive, e := s.AttemptResult(t.Context(), id, 1)
			if e != nil || archive.Result == nil || len(archive.Result.Findings) != 3 {
				t.Fatal("raw findings lost", e)
			}
			matching, e := contracts.DecodeTokenCostEstimate(archive.Result.Extensions.Value["componentMatchingEstimate"], &archive.Result.Execution)
			if e != nil || matching.PricingSnapshot.Revision != 1 || matching.Usage == nil || matching.Usage.CacheWriteInputTokens != nil || matching.Usage.RequestCount == nil || *matching.Usage.RequestCount != 2 {
				t.Fatal("matching snapshot", e, matching)
			}
			artifactPath := "raw/codex-security/component-matching-token-usage.json"
			code, downloaded := getJSON(t, app.URL+fmt.Sprintf("/api/v1/scans/%d/attempts/1/artifacts?path=%s", id, artifactPath), token)
			if code != 200 {
				t.Fatal("matching usage artifact not downloadable", code, string(downloaded))
			}
			digest := sha256.Sum256(downloaded)
			found := false
			for _, artifact := range archive.Result.Artifacts {
				if string(artifact.RelativePath) == artifactPath {
					found = true
					if string(artifact.SHA256) != hex.EncodeToString(digest[:]) || int64(artifact.SizeBytes) != int64(len(downloaded)) {
						t.Fatal("matching artifact hash/size mismatch")
					}
				}
			}
			if !found {
				t.Fatal("matching artifact absent from committed manifest")
			}
			budget, e := s.BudgetSnapshot(t.Context(), id)
			if e != nil || !budget.Attempts[0].TokenEstimate.Present {
				t.Fatal(e, budget)
			}
			estimate := budget.Attempts[0].TokenEstimate.Value
			complete := scenario == "complete" || scenario == "change-price"
			if complete {
				if matching.Status != "estimated" || matching.AmountMicros == nil || *matching.AmountMicros != 49 || estimate.Status != "estimated" || estimate.AmountMicros == nil || *estimate.AmountMicros != 78 || budget.Attempts[0].FinalMicros == nil || *budget.Attempts[0].FinalMicros != 78 || len(budget.UnknownAttempts) != 0 {
					t.Fatal("complete frozen-price accounting", matching, budget)
				}
			} else if matching.Status == "estimated" || estimate.Status == "estimated" || budget.Attempts[0].FinalMicros != nil || len(budget.UnknownAttempts) != 1 {
				t.Fatal("incomplete usage settled as a total", matching, budget)
			}
			views, e := s.ComponentAttempts(t.Context(), id)
			if e != nil || len(views) != 1 || views[0].MatchingEstimate == nil || !reflect.DeepEqual(*views[0].MatchingEstimate, matching) {
				t.Fatal("matching API projection", e, views)
			}
			code, body := getJSON(t, app.URL+fmt.Sprintf("/api/v1/scans/%d/component-executions", id), token)
			if code != 200 || !strings.Contains(string(body), `"matchingEstimate"`) {
				t.Fatal(code, body)
			}
			if matchingCalls.Load() != 2 || allCalls.Load() != 5 {
				t.Fatal("fixture requests replayed or double counted", matchingCalls.Load(), allCalls.Load())
			}
			if scenario == "complete" {
				historicalBudget = budget
				for _, mutate := range []func(*contracts.TokenCostEstimate){
					func(v *contracts.TokenCostEstimate) { n := *v.AmountMicros + 1; v.AmountMicros = &n },
					func(v *contracts.TokenCostEstimate) { v.PricingSnapshot.Revision++ },
					func(v *contracts.TokenCostEstimate) {
						rate := contracts.TokenUnitPrice("99")
						v.PricingSnapshot.Pricing.InputPerMillion = &rate
					},
				} {
					bytes, _ := contracts.EncodeJSON(*archive.Completion)
					forged, e := contracts.DecodeExecutionCompletion(bytes, &archive.Result.Execution)
					if e != nil {
						t.Fatal(e)
					}
					estimate, _ := contracts.DecodeTokenCostEstimate(forged.Result.Value.Extensions.Value["componentMatchingEstimate"], &forged.Execution)
					mutate(&estimate)
					forged.Result.Value.Extensions.Value["componentMatchingEstimate"], _ = json.Marshal(estimate)
					if e = s.CompleteContractExecution(t.Context(), forged); !errors.Is(e, store.ErrInvalidResult) {
						t.Fatal("forged amount or frozen price was not rejected at the validation boundary", e)
					}
				}
			}
			if first := ids["complete"]; first != 0 {
				old, e := s.BudgetSnapshot(t.Context(), first)
				if e != nil || !reflect.DeepEqual(old, historicalBudget) {
					t.Fatal("earlier price or cost changed", e)
				}
			}
		})
	}
	if os.Getenv("AXIOM_TEST_WEB_COMPONENT_MATCHING") == "1" {
		data, _ := json.Marshal(ids)
		cmd := exec.Command("node", "tests/component-matching-web-acceptance.mjs")
		cmd.Dir = filepath.Join(root, "code-audit-web")
		cmd.Env = append(os.Environ(), "AXIOM_TEST_BASE_URL="+app.URL, "AXIOM_TEST_TOKEN="+token, "AXIOM_TEST_COMPONENT_TASKS="+string(data))
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(output))
		} else {
			t.Log(string(output))
		}
	}
}
