package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestModelPricingAPIValidationAndHistory(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "pricing.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("fixture", "fixture-password")); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture"}).Routes())
	defer server.Close()
	token := login(t, server.URL, "admin", "fixture-password")
	price := map[string]any{"schemaVersion": 1, "currency": "USD", "unit": "per_million_tokens", "cacheMode": "cache_read", "inputPerMillion": "1", "cacheReadPerMillion": "0.1", "cacheWritePerMillion": nil, "outputPerMillion": "4", "note": "Fixtures / Mock only"}
	payload := map[string]any{"name": "Fixtures / Mock priced model", "provider": "deepseek", "model": "fixture-deepseek", "baseUrl": "https://fixture.invalid", "apiKey": "fixture-not-a-real-model-key", "wireApi": "responses", "pricing": price}
	code, raw := doJSON(t, server.URL+"/api/v1/model-configs", token, payload)
	if code != 200 {
		t.Fatal(code, raw)
	}
	var saved struct{ Data store.ModelConfigRow }
	if e = json.Unmarshal([]byte(raw), &saved); e != nil {
		t.Fatal(e)
	}
	if saved.Data.Pricing == nil || saved.Data.PricingRevision != 1 || saved.Data.WireAPI != "responses" {
		t.Fatal(saved.Data)
	}
	first, e := s.GetModelConfig(t.Context(), saved.Data.ID)
	if e != nil {
		t.Fatal(e)
	}
	endpoint := server.URL + fmt.Sprintf("/api/v1/model-configs/%d", first.ID)
	delete(payload, "apiKey")
	delete(payload, "pricing")
	code, raw = putSDKModelJSON(t, endpoint, token, payload)
	if code != 200 {
		t.Fatal(code, raw)
	}
	row, e := s.GetModelConfig(t.Context(), first.ID)
	if e != nil || row.Pricing == nil || row.PricingRevision != 1 || row.ApiKeyEncrypted != first.ApiKeyEncrypted {
		t.Fatal("old client erased prices or credentials", e, row)
	}
	for _, bad := range []any{-1, "-1", "0.0000001", "1e3", "1000000000"} {
		price["inputPerMillion"] = bad
		payload["pricing"] = price
		code, raw = putSDKModelJSON(t, endpoint, token, payload)
		if code != 400 {
			t.Fatal("invalid price accepted", bad, code, raw)
		}
	}
	price["inputPerMillion"] = "2"
	payload["pricing"] = price
	code, raw = putSDKModelJSON(t, endpoint, token, payload)
	if code != 200 {
		t.Fatal(code, raw)
	}
	row, e = s.GetModelConfig(t.Context(), first.ID)
	if e != nil || row.PricingRevision != 2 || string(*row.Pricing.InputPerMillion) != "2" {
		t.Fatal(e, row)
	}
	payload["wireApi"] = "chat"
	code, _ = putSDKModelJSON(t, endpoint, token, payload)
	if code != 400 {
		t.Fatal("pricing enabled Chat protocol", code)
	}
	payload["wireApi"] = "responses"
	payload["pricing"] = nil
	code, raw = putSDKModelJSON(t, endpoint, token, payload)
	if code != 200 {
		t.Fatal(code, raw)
	}
	row, e = s.GetModelConfig(t.Context(), first.ID)
	if e != nil || row.Pricing != nil || row.PricingRevision != 3 || row.ApiKeyEncrypted != first.ApiKeyEncrypted {
		t.Fatal("explicit removal or credential preservation failed", e, row)
	}
}
