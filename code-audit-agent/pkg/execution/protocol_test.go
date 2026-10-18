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

func TestScanInputBundleRejectsMissingIdentityAndUnsupportedScanner(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "wrong-identity", "unversioned", "old-scanner"} {
		t.Run(scenario, func(t *testing.T) {
			a := fixtureAssignment(t)
			sidecar := &contracts.ScanInputAssignment{SchemaVersion: 2, Execution: a.Execution, Input: contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}}
			v := control.ObservationLease{InputContractVersion: 2, ContractLease: control.ContractLease{InputLease: control.InputLease{Assignment: &a, SourcePath: "/source", OutputPath: "/output", LeaseSeconds: int(a.Lease.LeaseSeconds)}, ResultContractVersion: 1}, ObservationContractVersion: 1, ScanInput: sidecar}
			switch scenario {
			case "missing":
				v.ScanInput = nil
			case "wrong-identity":
				v.ScanInput.Execution.Attempt++
			case "unversioned":
				v.InputContractVersion = 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": v})
			}))
			defer server.Close()
			client := richClient(server.URL)
			if scenario != "old-scanner" {
				client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 4)
			}
			lease, e := client.Claim(t.Context(), string(a.Lease.AgentID))
			if scenario == "valid" {
				if e != nil || lease.ScanInput == nil {
					t.Fatal(e, lease)
				}
			} else if e == nil {
				t.Fatal("incompatible or mismatched sidecar was accepted")
			}
		})
	}
}

func TestSDKOperationBundleNeverFallsBackToScan(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-operation", "missing-input", "wrong-execution", "old-scanner", "unmarked"} {
		t.Run(scenario, func(t *testing.T) {
			a := fixtureAssignment(t)
			op := &contracts.SDKOperation{SchemaVersion: 1, Execution: a.Execution, Kind: "preflight"}
			v := control.ObservationLease{InputContractVersion: 3, ScanInput: &contracts.ScanInputAssignment{SchemaVersion: 2, Execution: a.Execution, Input: contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}}, SDKOperation: op, ContractLease: control.ContractLease{InputLease: control.InputLease{Assignment: &a, SourcePath: "/source", OutputPath: "/output", LeaseSeconds: int(a.Lease.LeaseSeconds)}, ResultContractVersion: 1}, ObservationContractVersion: 1}
			switch scenario {
			case "missing-operation":
				v.SDKOperation = nil
			case "missing-input":
				v.ScanInput = nil
			case "wrong-execution":
				op.Execution.Attempt++
			case "unmarked":
				v.InputContractVersion = 2
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": v})
			}))
			defer server.Close()
			client := richClient(server.URL)
			client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 4)
			if scenario != "old-scanner" {
				client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 5)
			}
			lease, e := client.Claim(t.Context(), string(a.Lease.AgentID))
			if scenario == "valid" {
				if e != nil || lease.SDKOperation == nil {
					t.Fatal(e, lease)
				}
			} else if e == nil {
				t.Fatal("SDK operation bundle was silently downgraded")
			}
		})
	}
}

func TestGitDiffLeaseRequiresVersionedBindingWithoutFallback(t *testing.T) {
	for _, scenario := range []string{"valid", "preflight", "missing", "wrong-execution", "wrong-source", "wrong-snapshot", "unmarked", "old-scanner", "missing-input", "preflight-old-operation"} {
		t.Run(scenario, func(t *testing.T) {
			a := fixtureAssignment(t)
			raw, e := os.ReadFile("../../../contracts/fixtures/v1/git-assignment.json")
			if e != nil {
				t.Fatal(e)
			}
			binding, e := contracts.DecodeGitDiffAssignment(raw, nil)
			if e != nil {
				t.Fatal(e)
			}
			binding.Execution = a.Execution
			binding.SourceSnapshotID = a.Source.SnapshotID
			binding.SourceContentHash = a.Source.ContentHash
			v := control.ObservationLease{InputContractVersion: contracts.GitDiffInputVersion, ScanInput: &contracts.ScanInputAssignment{SchemaVersion: 2, Execution: a.Execution, Input: contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}}, GitDiff: &binding, ContractLease: control.ContractLease{InputLease: control.InputLease{Assignment: &a, SourcePath: "/source", OutputPath: "/output", LeaseSeconds: int(a.Lease.LeaseSeconds)}, ResultContractVersion: 1}, ObservationContractVersion: 1}
			switch scenario {
			case "preflight", "preflight-old-operation":
				v.SDKOperation = &contracts.SDKOperation{SchemaVersion: 1, Execution: a.Execution, Kind: "preflight"}
			case "missing":
				v.GitDiff = nil
			case "wrong-execution":
				binding.Execution.Attempt++
			case "wrong-source":
				binding.SourceContentHash = contracts.Digest(string(a.Source.ContentHash[:63]) + "0")
			case "wrong-snapshot":
				binding.SourceSnapshotID = "999"
			case "unmarked":
				v.InputContractVersion = contracts.ScanInputVersion
			case "missing-input":
				v.ScanInput = nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": v})
			}))
			defer server.Close()
			client := richClient(server.URL)
			client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 4)
			if scenario != "preflight-old-operation" {
				client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 5)
			}
			if scenario != "old-scanner" {
				client.ObservationCapabilities.JobVersions = append(client.ObservationCapabilities.JobVersions, 6)
			}
			lease, e := client.Claim(t.Context(), string(a.Lease.AgentID))
			if scenario == "valid" || scenario == "preflight" {
				if e != nil || lease.GitDiff == nil || lease.GitDiff.Target != binding.Target {
					t.Fatal("Git binding lost", e, lease)
				}
			} else if e == nil {
				t.Fatal("invalid Git diff lease accepted", scenario)
			}
		})
	}
}
