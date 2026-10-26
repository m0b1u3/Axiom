package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestModelConnectionDoesNotCertifyOrEraseSDKCompatibility(t *testing.T) {
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	for _, initial := range []string{"experimental", "supported"} {
		for _, status := range []int{200, 400, 503} {
			t.Run(fmt.Sprintf("%s-%d", initial, status), func(t *testing.T) {
				calls := 0
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/responses" {
						t.Errorf("unexpected protocol path %s", r.URL.Path)
					}
					if r.Header.Get("Authorization") != "Bearer fixture-only-not-real" {
						t.Error("wrong fixture credential")
					}
					var body map[string]any
					if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
						t.Error(e)
					}
					if body["tools"] != nil || body["text"] != nil {
						t.Error("connectivity probe silently became a capability test")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprint(w, `{"fixtureOnly":true}`)
				}))
				defer provider.Close()
				s, e := store.Open(filepath.Join(t.TempDir(), "fixture.db"))
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close()
				key, e := cryptokey.Encrypt("fixture-only-not-real")
				if e != nil {
					t.Fatal(e)
				}
				model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock", Provider: "custom", Model: "fixture-model", BaseURL: provider.URL, WireAPI: "responses", ApiKeyEncrypted: key, CompatibilityStatus: initial})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("fixture-secret", "fixture-password")); e != nil {
					t.Fatal(e)
				}
				web := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture-secret"}).Routes())
				defer web.Close()
				token := login(t, web.URL, "admin", "fixture-password")
				code, body := doJSON(t, fmt.Sprintf("%s/api/v1/model-configs/%d/test", web.URL, model.ID), token, map[string]any{})
				if code != 200 {
					t.Fatalf("HTTP %d %s", code, body)
				}
				var result struct {
					Data struct {
						OK       bool   `json:"ok"`
						Scope    string `json:"scope"`
						Verified bool   `json:"compatibilityVerified"`
						Message  string `json:"message"`
					}
				}
				if e = json.Unmarshal([]byte(body), &result); e != nil {
					t.Fatal(e)
				}
				if result.Data.OK != (status == 200) || result.Data.Scope != "connectivity_only" || result.Data.Verified {
					t.Fatalf("wrong probe result %s", body)
				}
				if status == 200 && result.Data.Message == "" {
					t.Fatal("missing capability boundary")
				}
				after, e := s.GetModelConfig(t.Context(), model.ID)
				if e != nil {
					t.Fatal(e)
				}
				if after.CompatibilityStatus != initial || after.ApiKeyEncrypted != model.ApiKeyEncrypted || after.UpdatedAt != model.UpdatedAt {
					t.Fatal("connectivity probe mutated configuration or compatibility")
				}
				if calls != 1 {
					t.Fatalf("probe retried %d times", calls)
				}
			})
		}
	}
}
