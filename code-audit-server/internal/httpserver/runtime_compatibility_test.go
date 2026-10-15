package httpserver_test

import (
	"axiom/code-audit-server/internal/httpserver"
	"axiom/contracts/control"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeDiscoveryAuthAndInvalidContracts(t *testing.T) {
	api := (&httpserver.Server{AgentToken: "fixture-runtime-token"}).Routes()
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/api/internal/executions/describe", "", "wrong"); w.Code != 401 {
		t.Fatal("unauthenticated discovery", w.Code)
	}
	w := request(http.MethodGet, "/api/internal/executions/describe", "", "fixture-runtime-token")
	var envelope struct{ Data control.ServerDescription }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Data.MatrixHash != control.MatrixHash || len(envelope.Data.ControlAPIs) != len(control.ControlAPIs()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid descriptor", w.Code, w.Body.String())
	}
	for _, raw := range []string{
		`{"agentId":"fixture","runtime":{"schemaVersion":2}}`,
		`{"agentId":"fixture","runtime":{"schemaVersion":1,"state":"failed"}}`,
		`{"agentId":"fixture","runtime":{"schemaVersion":1,"state":"blocked","diagnostic":null}}`,
		`{"agentId":"fixture","runtime":null,"unknown":true}`,
	} {
		if w := request(http.MethodPost, "/api/internal/executions/runtime", raw, "fixture-runtime-token"); w.Code != 400 {
			t.Fatal("invalid metadata reached Store", w.Code, w.Body.String())
		}
	}
}
