package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeConcurrencyLeaseRequiresNegotiatedRuntime(t *testing.T) {
	c := richClient("")
	c.ObservationCapabilities.JobVersions = []int64{3, 4, 8}
	lease := &Lease{Execution: contracts.ExecutionRef{TaskID: "42", Attempt: 1}, Assignment: &contracts.ExecutionAssignment{SchemaVersion: 1, Policy: contracts.ScanPolicy{Workers: contracts.Some(int64(65))}}, ResultContractVersion: 1, ObservationContractVersion: 1}
	if _, e := c.validateLease(lease); e == nil {
		t.Fatal("old runtime accepted a widened worker configuration")
	}
	c.SDKConcurrencyVersion = 1
	if _, e := c.validateLease(lease); e != nil {
		t.Fatal(e)
	}
	lease.Assignment.Policy.Workers = contracts.Some(int64(64))
	c.SDKConcurrencyVersion = 0
	if _, e := c.validateLease(lease); e != nil {
		t.Fatal("historical range rejected", e)
	}
}

func TestNativeConcurrencyRequiresKnownServerVersion(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/internal/executions/describe" {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ServerDescription{ControlAPIs: control.ControlAPIs(), MatrixHash: control.MatrixHash, SDKConcurrencyVersion: version}})
				return
			}
			w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
		}))
		c := richClient(server.URL)
		c.SDKConcurrencyVersion = 1
		if e := c.Negotiate(context.Background(), "fixture-native-concurrency", 1); e != nil {
			t.Fatal(e)
		}
		want := 0
		if version == 1 {
			want = 1
		}
		if c.SDKConcurrencyVersion != want {
			t.Fatal(version, c.SDKConcurrencyVersion)
		}
		server.Close()
	}
}
