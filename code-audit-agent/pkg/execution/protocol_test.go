package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func fixtureAssignment(t *testing.T) contracts.ExecutionAssignment {
	t.Helper()
	raw, err := os.ReadFile("../../../contracts/fixtures/v1/assignment.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := contracts.DecodeExecutionAssignment(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestClaimUsesSharedEnvelopesForEveryExistingVersion(t *testing.T) {
	a := fixtureAssignment(t)
	agent := string(a.Lease.AgentID)
	input := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	result := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	observations := contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	for _, version := range []string{"legacy", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			path := "/api/internal/" + version + "/executions/claim"
			var payload any = control.InputLease{Assignment: &a, SourcePath: "/fixture/source", OutputPath: "/fixture/output", LeaseSeconds: int(a.Lease.LeaseSeconds)}
			if version == "legacy" {
				path = "/api/internal/executions/claim"
				payload = control.LegacyLease{Task: &control.LegacyTask{ID: 9007199254740993, Attempt: 2, AgentID: agent, SourcePath: "/fixture/source", OutputPath: "/fixture/output"}, LeaseSeconds: 60, CredentialEnv: "FIXTURE_VENDOR_API_KEY", Credential: "fixture", WireAPI: "chat", DisplayName: "Fixture"}
			} else if version == "v3" {
				payload = control.ContractLease{InputLease: payload.(control.InputLease), ResultContractVersion: 1}
			} else if version == "v4" {
				payload = control.ObservationLease{ContractLease: control.ContractLease{InputLease: payload.(control.InputLease), ResultContractVersion: 1}, ObservationContractVersion: 1}
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("request contract changed", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": payload})
			}))
			defer s.Close()
			client := NewClient(s.URL, "fixture-token")
			if version != "legacy" {
				client.InputCapabilities = &input
			}
			if version == "v3" || version == "v4" {
				client.ResultCapabilities = &result
			}
			if version == "v4" {
				client.ObservationCapabilities = &observations
			}
			lease, err := client.Claim(context.Background(), agent)
			if err != nil {
				t.Fatal(err)
			}
			if !lease.HasTask() || lease.NumericTaskID() != 9007199254740993 || lease.Execution.Attempt != 2 || lease.SourcePath != "/fixture/source" {
				t.Fatalf("identity lost: %+v", lease)
			}
			if version != "legacy" && (lease.Assignment == nil || lease.Legacy != nil || !reflect.DeepEqual(lease.Assignment.Model, a.Model)) {
				t.Fatal("modern assignment passed through legacy persistence fields")
			}
			if version == "v4" && (lease.ResultContractVersion != 1 || lease.ObservationContractVersion != 1) {
				t.Fatal("negotiated versions lost")
			}
		})
	}
}

func TestInputLeaseRejectsWrongIdentityAndAmbiguousRepresentations(t *testing.T) {
	a := fixtureAssignment(t)
	v := control.InputLease{Assignment: &a, SourcePath: "/source", OutputPath: "/output", LeaseSeconds: int(a.Lease.LeaseSeconds)}
	if _, err := leaseFromInput(v, "another-agent"); err == nil {
		t.Fatal("wrong agent accepted")
	}
	v.LeaseSeconds++
	if _, err := leaseFromInput(v, string(a.Lease.AgentID)); err == nil {
		t.Fatal("inconsistent lease accepted")
	}
	v.LeaseSeconds--
	v.Legacy = &control.LegacyLease{}
	if _, err := leaseFromInput(v, string(a.Lease.AgentID)); err == nil {
		t.Fatal("ambiguous lease accepted")
	}
	if l, err := leaseFromInput(control.InputLease{}, "fixture"); err != nil || l.HasTask() {
		t.Fatal("empty queue response changed")
	}
}
