package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResponsesOnlyModelConfigurationAndProbe(t *testing.T) {
	st, _, web, _ := securityFixture(t)
	token := login(t, web.URL, "admin", "fixture-admin-password")
	payload := map[string]any{"name": "Fixtures / Mock only", "provider": "openai", "model": "fixture-no-model-calls", "sdkIdentityOnly": true, "wireApi": "chat"}
	if code, body := doJSON(t, web.URL+"/api/v1/model-configs", token, payload); code != 400 || !strings.Contains(body, "Responses") && !strings.Contains(body, "responses") {
		t.Fatal(code, body)
	}
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("Retired Chat configuration contacted upstream")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	legacy, err := st.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock retired Chat", Provider: "custom", Model: "fixture-only", WireAPI: "chat", BaseURL: upstream.URL, ApiKeyEncrypted: "fixture-not-decryptable"})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doJSON(t, fmt.Sprintf("%s/api/v1/model-configs/%d/test", web.URL, legacy.ID), token, map[string]any{}); code != 400 || !strings.Contains(body, "Chat 协议适配已移除") {
		t.Fatal(code, body)
	}
	update := map[string]any{"name": legacy.Name, "provider": "custom", "model": legacy.Model, "baseUrl": upstream.URL, "wireApi": "chat"}
	if code, body := putSDKModelJSON(t, fmt.Sprintf("%s/api/v1/model-configs/%d", web.URL, legacy.ID), token, update); code != 400 {
		t.Fatal(code, body)
	}
	payload["wireApi"] = "responses"
	payload["baseUrl"] = upstream.URL + "/chat/completions"
	if code, body := doJSON(t, web.URL+"/api/v1/model-configs", token, payload); code != 400 {
		t.Fatal(code, body)
	}
	if calls.Load() != 0 {
		t.Fatal("Unexpected upstream request")
	}
}

func TestResponsesOnlyOfflineDiagnosticAdmission(t *testing.T) {
	_, server, web, _ := securityFixture(t)
	server.QueueLimits.OutputRoot = t.TempDir() + "/scans"
	token := login(t, web.URL, "admin", "fixture-admin-password")
	request := map[string]any{"schemaVersion": 1, "mode": "offline", "catalogSha256": contracts.StructuredDiagnosticCatalogHash, "protocol": "chat", "scenario": "title_only"}
	if code, body := doJSON(t, web.URL+"/api/v1/diagnostics/structured-output/records", token, request); code != 400 || !strings.Contains(body, "Responses") {
		t.Fatal(code, body)
	}
}
