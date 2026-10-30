package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestInputBudgetAndScopeValidation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, _ = st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("fixture", "pass"))
	_, err = st.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture.zip", ArchiveSha256: strings.Repeat("0", 64), ContentHash: "1111111111111111111111111111111111111111111111111111111111111111", SnapshotPath: "/fixture/source"})
	if err != nil {
		t.Fatal(err)
	}
	rate := contracts.TokenUnitPrice("1")
	model, err := st.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock budget", Provider: "openai", Model: "fixture-local", WireAPI: "responses", ApiKeyEncrypted: "fixture-not-a-real-key", Pricing: &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: &rate, OutputPerMillion: &rate}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&httpserver.Server{Store: st, Secret: "fixture"}).Routes())
	defer ts.Close()
	token := login(t, ts.URL, "admin", "pass")
	for _, v := range []struct {
		name  string
		extra map[string]any
		want  int
	}{
		{"unset", map[string]any{}, 200}, {"null", map[string]any{"maxCostUsd": nil}, 200}, {"positive", map[string]any{"maxCostUsd": 4.25}, 200},
		{"sub-micro", map[string]any{"maxCostUsd": 0.0000001}, 400}, {"huge", map[string]any{"maxCostUsd": 9007199255.0}, 400}, {"zero", map[string]any{"maxCostUsd": 0}, 400}, {"negative", map[string]any{"maxCostUsd": -1}, 400}, {"string", map[string]any{"maxCostUsd": "5"}, 400},
		{"absolute", map[string]any{"includePaths": []string{"/etc/passwd"}}, 400}, {"duplicate", map[string]any{"includePaths": []string{"src/a.ts", "src\\a.ts"}}, 400},
		{"scope", map[string]any{"includePaths": []string{"src/a,b.ts", "源码/测试.ts"}}, 200}, {"worker-standard", map[string]any{"workers": 2}, 400},
		{"worker-fraction", map[string]any{"scanMode": "deep", "workers": 1.5}, 400}, {"deep", map[string]any{"scanMode": "deep", "workers": 3}, 200},
		{"unknown", map[string]any{"unsupportedOption": true}, 400}, {"mode", map[string]any{"scanMode": "invalid"}, 400},
	} {
		t.Run(v.name, func(t *testing.T) {
			body := map[string]any{"sourceSnapshotId": 1, "modelConfigId": model.ID}
			for k, x := range v.extra {
				body[k] = x
			}
			code, raw := doJSON(t, ts.URL+"/api/v1/scans", token, body)
			if code != v.want {
				t.Fatalf("%s got %d %s", v.name, code, raw)
			}
			if code == 200 {
				var data struct {
					Data store.Task `json:"data"`
				}
				if err := json.Unmarshal([]byte(raw), &data); err != nil {
					t.Fatal(err)
				}
				if data.Data.InputContractVersion != 1 {
					t.Fatal("new snapshot task did not select job input")
				}
				if v.name == "positive" && (data.Data.MaxCostUSD == nil || *data.Data.MaxCostUSD != 4.25) {
					t.Fatal("budget was lost")
				}
				if v.name == "null" && data.Data.MaxCostUSD != nil {
					t.Fatal("unset budget became a default")
				}
			}
		})
	}
}
