package control

import (
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestGeneratedMatrixMatchesCanonicalData(t *testing.T) {
	raw, err := os.ReadFile("../../data/execution-compatibility.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		MatrixVersion int
		ControlAPIs   []int
		Profiles      []Profile
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != MatrixHash || data.MatrixVersion != MatrixVersion || !reflect.DeepEqual(data.Profiles, Profiles()) || !reflect.DeepEqual(data.ControlAPIs, ControlAPIs()) {
		t.Fatal("generated matrix drifted")
	}
	copy := Profiles()
	copy[0].ID = "changed"
	if Profiles()[0].ID == "changed" {
		t.Fatal("caller mutated matrix")
	}
}

func TestEveryCapabilityCombinationAgainstEveryTaskProfile(t *testing.T) {
	for mask := 0; mask < 512; mask++ {
		c := Capabilities{Result: contracts.NoResultCapabilities(), Observation: contracts.NoObservationCapabilities()}
		if mask&1 != 0 {
			c.Input = contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}}
		}
		c.Input.MaxCostUSD = mask&2 != 0
		if mask&4 != 0 {
			c.Result = contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
		}
		if mask&8 != 0 {
			c.Observation = contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
		}
		if mask&16 != 0 {
			c.Observation.JobVersions = append(c.Observation.JobVersions, 4)
		}
		if mask&32 != 0 {
			c.Observation.JobVersions = append(c.Observation.JobVersions, 5)
		}
		if mask&64 != 0 {
			c.Observation.JobVersions = append(c.Observation.JobVersions, 6)
		}
		if mask&128 != 0 {
			c.Observation.JobVersions = append(c.Observation.JobVersions, 7)
		}
		if mask&256 != 0 {
			c.Observation.JobVersions = append(c.Observation.JobVersions, 8)
		}
		for _, p := range Profiles() {
			for _, budget := range []bool{false, true} {
				v := Requirements{Input: p.Input, Result: p.Result, Observation: p.Observation, Budget: budget}
				want := (!budget || (p.Input > 0 && mask&2 != 0)) && (p.Input == 0 || mask&1 != 0) && (p.Result == 0 || mask&4 != 0) && (p.Observation == 0 || mask&8 != 0) && ((p.Input != 2 && p.Input != 3 && p.Input != 4 && p.Input != 5 && p.Input != 6) || mask&16 != 0) && (p.Input != 3 || mask&32 != 0) && (p.Input != 4 || mask&64 != 0) && (p.Input != 5 || mask&128 != 0) && (p.Input != 6 || mask&256 != 0)
				if c.ReadyFor(v) != want {
					t.Fatalf("mask=%d profile=%s budget=%t", mask, p.ID, budget)
				}
			}
		}
		for _, unknown := range []Requirements{{Input: 9}, {Input: 1, Result: 9}, {Input: 1, Result: 1, Observation: 9}, {Input: 0, Result: 1}, {Input: 1, Observation: 1}} {
			if c.ReadyFor(unknown) {
				t.Fatal("unknown task combination admitted", unknown)
			}
		}
	}
}

func TestGitDiffOptionalOperationRequiresItsOwnDeclaredCapability(t *testing.T) {
	c := Capabilities{Input: contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}}, Result: contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}, Observation: contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 6}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}}
	v := Requirements{Input: 4, Result: 1, Observation: 1, ScanInput: true}
	if !c.ReadyFor(v) {
		t.Fatal("ordinary Git diff rejected")
	}
	v.SDKOperation = true
	if c.ReadyFor(v) {
		t.Fatal("Git operation admitted without operation support")
	}
	c.Observation.JobVersions = append(c.Observation.JobVersions, 5)
	if !c.ReadyFor(v) {
		t.Fatal("complete Git operation capability rejected")
	}
	v.Input = 2
	if c.ReadyFor(v) {
		t.Fatal("unmarked operation accepted")
	}
}
