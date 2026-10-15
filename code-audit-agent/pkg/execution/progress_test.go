package execution

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHeartbeatUsesGeneratedObjectAndPreservesCancelResponse(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/observation-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	p, e := contracts.DecodeProgressSnapshotV2(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/v4/executions/heartbeat" {
			t.Error(r.URL.Path)
		}
		var body map[string]json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if len(body["progress"]) == 0 || body["progress"][0] != '{' || body["progressJson"] != nil {
			t.Error("stringified legacy progress crossed new boundary")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"status":"cancel_requested","revision":2,"accepted":true}}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "fixture-token")
	status, e := client.HeartbeatContract(context.Background(), contracts.ExecutionHeartbeat{SchemaVersion: 1, Execution: p.Execution, AgentID: "fixture-agent", Progress: &p})
	if e != nil || status != "cancel_requested" {
		t.Fatal(status, e)
	}
}
