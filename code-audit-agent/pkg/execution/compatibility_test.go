package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func richClient(url string) *Client {
	c := NewClient(url, "fixture")
	c.InputCapabilities = &contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	c.ResultCapabilities = &contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	c.ObservationCapabilities = &contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	return c
}
func TestNegotiateOldServerWithPresenceOnly(t *testing.T) {
	for _, api := range []int{1, 2, 3, 4} {
		t.Run(string(rune('0'+api)), func(t *testing.T) {
			claims := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/claim") {
					claims++
				}
				path := "/api/internal/executions/presence"
				if api > 1 {
					path = "/api/internal/v" + string(rune('0'+api)) + "/executions/presence"
				}
				if r.URL.Path != path {
					w.WriteHeader(404)
					return
				}
				w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
			}))
			defer s.Close()
			c := richClient(s.URL)
			if e := c.Negotiate(context.Background(), "fixture-agent", 1); e != nil {
				t.Fatal(e)
			}
			if c.ControlAPI != api || claims != 0 || c.RuntimeEndpoint {
				t.Fatal("unsafe discovery", c.ControlAPI, claims)
			}
		})
	}
}
func TestDiscoveryDoesNotDowngradeAuthNetworkOrInvalidMetadata(t *testing.T) {
	for _, status := range []int{401, 403, 500, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				w.Write([]byte(`{"code":0,"data":{"controlAPIs":[4],"matrixHash":"wrong"}}`))
			}))
			defer s.Close()
			if e := richClient(s.URL).Negotiate(context.Background(), "fixture", 1); e == nil || calls != 1 {
				t.Fatal("unsafe fallback", e, calls)
			}
		})
	}
}
func TestNegotiatedClaimNeverFallsBackAndRejectsChangedContracts(t *testing.T) {
	for _, status := range []int{404, 503, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			paths := []string{}
			a := fixtureAssignment(t)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.WriteHeader(status)
				if status == 200 {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ObservationLease{ContractLease: control.ContractLease{InputLease: control.InputLease{Assignment: &a, LeaseSeconds: 60}, ResultContractVersion: 9}, ObservationContractVersion: 1}})
				}
			}))
			defer s.Close()
			c := richClient(s.URL)
			c.ControlAPI = 4
			if _, e := c.Claim(context.Background(), "fixture-agent"); e == nil {
				t.Fatal("unsupported contracts accepted")
			}
			if len(paths) != 1 || paths[0] != "/api/internal/v4/executions/claim" {
				t.Fatal("claim downgraded", paths)
			}
		})
	}
}

func TestPresenceDiscoveryOnlyDowngradesMissingRoutes(t *testing.T) {
	for _, status := range []int{400, 401, 403, 500, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/describe") {
					w.WriteHeader(404)
					return
				}
				calls++
				w.WriteHeader(status)
				w.Write([]byte(`{"code":40000,"data":{"error":{"code":"protocol_version_unsupported","origin":"server","message":"fixture unsupported format","retryable":false}}}`))
			}))
			defer s.Close()
			if e := richClient(s.URL).Negotiate(context.Background(), "fixture", 1); e == nil || calls != 1 {
				t.Fatal("existing presence route incorrectly downgraded", e, calls)
			}
		})
	}
}

func TestMatrixMismatchPublishesDiagnosticWithoutClaiming(t *testing.T) {
	var reported *contracts.AgentRuntime
	claims := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/describe"):
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ServerDescription{ControlAPIs: control.ControlAPIs(), MatrixHash: strings.Repeat("a", 64)}})
		case strings.HasSuffix(r.URL.Path, "/presence"):
			w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
		case strings.HasSuffix(r.URL.Path, "/runtime"):
			var v control.RuntimeReport
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
				t.Error(e)
			}
			reported = &v.Runtime
			w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
		default:
			claims++
			w.WriteHeader(500)
		}
	}))
	defer s.Close()
	if e := richClient(s.URL).Negotiate(context.Background(), "fixture-matrix-node", 1); e == nil {
		t.Fatal("mismatched matrices accepted")
	}
	if claims != 0 || reported == nil || reported.State != "blocked" || reported.Diagnostic == nil || reported.Diagnostic.Code != "protocol_version_unsupported" || reported.MatrixHash != control.MatrixHash {
		t.Fatal("missing safe Web diagnostic", reported, claims)
	}
	raw, e := contracts.EncodeJSON(reported)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = contracts.DecodeAgentRuntime(raw, nil); e != nil {
		t.Fatal(e)
	}
}
