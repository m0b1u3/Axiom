package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestModelReasoningRoundTripAndOldClientCompatibility(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "models.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("fixture", "fixture-password")); e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture"}).Routes())
	defer ts.Close()
	token := login(t, ts.URL, "admin", "fixture-password")
	for _, effort := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "invented"} {
		code, body := doJSON(t, ts.URL+"/api/v1/model-configs", token, map[string]any{"name": "Fixtures / Mock " + effort, "provider": "openai", "model": "fixture-no-calls", "sdkIdentityOnly": true, "reasoningEffort": effort})
		if effort == "invented" {
			if code != 400 {
				t.Fatal(code, body)
			}
			continue
		}
		if code != 200 {
			t.Fatal(code, body)
		}
		var created struct{ Data store.ModelConfigRow }
		if e := json.Unmarshal([]byte(body), &created); e != nil {
			t.Fatal(e)
		}
		if created.Data.ReasoningEffort != effort {
			t.Fatal("Reasoning configuration not persisted", body)
		}
		// An older Web does not know this field. Omitting it must preserve the value.
		payload := map[string]any{"name": created.Data.Name, "provider": "openai", "model": "fixture-no-calls", "baseUrl": "", "wireApi": "responses"}
		update := func() {
			t.Helper()
			code, body = putSDKModelJSON(t, ts.URL+fmt.Sprintf("/api/v1/model-configs/%d", created.Data.ID), token, payload)
			if code != 200 {
				t.Fatal(code, body)
			}
		}
		update()
		saved, e := s.GetModelConfig(t.Context(), created.Data.ID)
		if e != nil || saved.ReasoningEffort != effort {
			t.Fatal("Old client erased effort", e, saved)
		}
		payload["reasoningEffort"] = ""
		update()
		saved, e = s.GetModelConfig(t.Context(), created.Data.ID)
		if e != nil || saved.ReasoningEffort != "" {
			t.Fatal("Explicit SDK default failed", e, saved)
		}
	}
}

func putSDKModelJSON(t *testing.T, url, token string, payload any) (int, string) {
	t.Helper()
	raw, e := json.Marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest(http.MethodPut, url, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, string(body)
}
