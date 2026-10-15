package store

import (
	"axiom/contracts"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPostgresInputDispatch(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	snapshot, err := s.InsertSnapshot(ctx, SnapshotRow{OriginalFilename: "fixture.zip", ArchiveSha256: strings.Repeat("0", 64), ContentHash: strings.Repeat("1", 64), SnapshotPath: "/fixture/source"})
	if err != nil {
		t.Fatal(err)
	}
	model := contracts.ResolvedModel{ProviderID: "openai", ConnectionType: contracts.ResolvedModelConnectionTypeOpenai, Model: "fixture-model", WireAPI: contracts.ResolvedModelWireAPIResponses, CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "fixture-revision"}
	policy := contracts.ScanPolicy{Mode: contracts.ScanModeStandard, TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: contracts.ValidationModeDefault}, MaxCostUSD: contracts.Some(4.25)}
	m, _ := json.Marshal(model)
	p, _ := json.Marshal(policy)
	budget := 4.25
	id, err := s.InsertPending(ctx, Task{ScanNo: "fixture-input", SourcePath: snapshot.SnapshotPath, SourceSnapshotID: snapshot.ID, SourceContentHash: snapshot.ContentHash, Provider: "openai", Model: model.Model, ScanMode: "standard", TimeoutMinutes: 1, MaxCostUSD: &budget, InputContractVersion: 1, ModelSnapshotJSON: string(m), PolicySnapshotJSON: string(p)})
	if err != nil {
		t.Fatal(err)
	}
	limits := ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: t.TempDir(), MaxRecoveries: 2}
	if task, err := s.ClaimExecution(ctx, "legacy", limits); err != nil || task != nil {
		t.Fatalf("legacy claimed new input: %v %v", task, err)
	}
	noBudget := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}}
	if task, err := s.ClaimExecutionWithInput(ctx, "no-budget", limits, noBudget); err != nil || task != nil {
		t.Fatalf("unsupported budget dispatched: %v %v", task, err)
	}
	if err = s.ReportAgent(ctx, "legacy", 1); err != nil {
		t.Fatal(err)
	}
	v, err := s.QueueOverview(ctx, limits, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tasks) != 1 || v.Tasks[0].Reason != "input_incompatible" {
		t.Fatalf("queue did not explain compatibility: %+v", v.Tasks)
	}
	caps := noBudget
	caps.MaxCostUSD = true
	if err = s.ReportAgentWithInput(ctx, "modern", 1, caps); err != nil {
		t.Fatal(err)
	}
	task, err := s.ClaimExecutionWithInput(ctx, "modern", limits, caps)
	if err != nil || task == nil || task.ID != id || task.MaxCostUSD == nil || *task.MaxCostUSD != budget {
		t.Fatalf("input dispatch failed: %+v %v", task, err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.InsertPending(ctx, Task{ScanNo: fmt.Sprint("fixture-old-", i), SourcePath: "/fixture/source", Provider: "openai", Model: "fixture-model"}); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := s.ClaimExecution(ctx, "legacy", limits)
	if err != nil || legacy == nil || legacy.MaxCostUSD != nil || legacy.InputContractVersion != 0 {
		t.Fatalf("legacy migration changed defaults: %+v %v", legacy, err)
	}
}
