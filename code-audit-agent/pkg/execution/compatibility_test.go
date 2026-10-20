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

func TestSDKIdentityCapabilityRequiresServerSupport(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/describe") {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ServerDescription{ControlAPIs: control.ControlAPIs(), MatrixHash: control.MatrixHash, SDKIdentityVersion: version}})
					return
				}
				w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
			}))
			defer server.Close()
			c := richClient(server.URL)
			c.SDKIdentityVersion = 1
			if e := c.Negotiate(t.Context(), "fixture-agent", 1); e != nil {
				t.Fatal(e)
			}
			if c.SDKIdentityVersion != version {
				t.Fatal("identity capability did not follow Server advertisement")
			}
		})
	}
}

func TestSDKParametersRequireExplicitServerSupport(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/describe") {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ServerDescription{ControlAPIs: control.ControlAPIs(), MatrixHash: control.MatrixHash, SDKParametersVersion: version}})
					return
				}
				w.Write([]byte(`{"code":0,"data":{"ok":true}}`))
			}))
			defer server.Close()
			client := richClient(server.URL)
			client.SDKParametersVersion = 1
			if e := client.Negotiate(t.Context(), "fixture", 1); e != nil {
				t.Fatal(e)
			}
			if client.SDKParametersVersion != version {
				t.Fatal("New SDK parameters were enabled on an older Server")
			}
		})
	}
}

func TestSDKParametersMissingOrUnknownSidecarNeverFallsBack(t *testing.T) {
	for _, version := range []int{1, 2} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ObservationLease{SDKParametersVersion: version}})
		}))
		client := richClient(server.URL)
		client.SDKParametersVersion = 1
		if _, e := client.claimObservations(t.Context(), "fixture"); e == nil {
			t.Fatal("Missing/unknown parameter sidecar accepted", version)
		}
		server.Close()
	}
}

func TestUnboundedDispatchRequiresExplicitServerPolicy(t *testing.T) {
	for _, mode := range []string{"", "future", control.DispatchUnbounded} {
		t.Run("policy-"+mode, func(t *testing.T) {
			presence := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/describe") {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": control.ServerDescription{TaskDispatch: mode, ControlAPIs: control.ControlAPIs(), MatrixHash: control.MatrixHash}})
					return
				}
				presence = true
				if r.URL.Path != "/api/internal/v4/executions/presence" {
					t.Error("unexpected request", r.URL.Path)
				}
				var payload control.ObservationPresence
				if e := json.NewDecoder(r.Body).Decode(&payload); e != nil || payload.DispatchMode != control.DispatchUnbounded || payload.Slots != 0 {
					t.Error("invalid unbounded presence", e, payload)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]bool{"ok": true}})
			}))
			defer server.Close()
			client := richClient(server.URL)
			client.DispatchMode = control.DispatchUnbounded
			err := client.Negotiate(t.Context(), "fixture-agent", 0)
			if mode == control.DispatchUnbounded {
				if err != nil || !presence {
					t.Fatal(err, presence)
				}
			} else if err == nil || presence {
				t.Fatal("unbounded dispatcher silently fell back", err, presence)
			}
		})
	}
	t.Run("missing-descriptor", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/describe") {
				t.Error("fallback presence attempted")
			}
			http.NotFound(w, r)
		}))
		defer server.Close()
		client := richClient(server.URL)
		client.DispatchMode = control.DispatchUnbounded
		if client.Negotiate(t.Context(), "fixture-agent", 0) == nil {
			t.Fatal("missing policy accepted")
		}
	})
}
