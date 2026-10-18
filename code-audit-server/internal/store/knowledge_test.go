package store

import (
	"axiom/contracts"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeVersionsAreImmutableAndArchivedReferencesRemainReadable(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := t.Context()
	first, e := s.SaveKnowledge(ctx, "", 0, "业务说明", "第一版\n合成正文")
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.SaveKnowledge(ctx, first.DocumentID, 1, "业务说明新版", "第二版")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveKnowledge(ctx, first.DocumentID, 1, "stale", "stale"); !errors.Is(e, ErrKnowledgeConflict) {
		t.Fatal("stale update", e)
	}
	old, e := s.KnowledgeVersion(ctx, first.ID)
	if e != nil || old.Content != first.Content || old.SHA256 != first.SHA256 || second.Version != 2 || old.LatestVersion != 2 {
		t.Fatal(e, old)
	}
	if e = s.ArchiveKnowledge(ctx, first.DocumentID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveKnowledge(ctx, []int64{first.ID}); !errors.Is(e, ErrKnowledgeConflict) {
		t.Fatal(e)
	}
	old, e = s.KnowledgeVersion(ctx, first.ID)
	if e != nil || old.Content != first.Content {
		t.Fatal("archive lost history", e)
	}
	page, e := s.ListKnowledge(ctx, "业务", "", true, 1)
	if e != nil || page.Total != 1 || page.Items[0].ID != second.ID {
		t.Fatal(e, page)
	}
	if e = s.ArchiveKnowledge(ctx, first.DocumentID, false); e != nil {
		t.Fatal(e)
	}
	docs, e := s.ResolveKnowledge(ctx, []int64{first.ID})
	if e != nil || docs[0].Content != first.Content {
		t.Fatal(e)
	}
	third, e := s.SaveKnowledge(ctx, first.DocumentID, old.LatestVersion, first.Title, first.Content+"\n由历史版本另存")
	if e != nil || third.Version != 3 {
		t.Fatal("historical edit must create a new latest version", e, third)
	}
	old, e = s.KnowledgeVersion(ctx, first.ID)
	if e != nil || old.Content != first.Content || old.LatestVersion != 3 {
		t.Fatal("historical body changed or latest revision was lost", e, old)
	}
	for i := 0; i < 53; i++ {
		if _, e = s.SaveKnowledge(ctx, "", 0, fmt.Sprintf("document %d", i), "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	page, e = s.ListKnowledge(ctx, "document", "", false, 2)
	if e != nil || page.Total != 53 || len(page.Items) != 3 {
		t.Fatal(e, page)
	}
}
func TestPostgresScanInputDispatchRequiresJob4AndPreservesFrozenKnowledge(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	first, e := s.SaveKnowledge(ctx, "", 0, "合成文档", "immutable")
	if e != nil {
		t.Fatal(e)
	}
	docs, e := s.ResolveKnowledge(ctx, []int64{first.ID})
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(ctx, SnapshotRow{OriginalFilename: "fixture.zip", ArchiveSha256: fmt.Sprintf("%064d", 0), ContentHash: fmt.Sprintf("%064d", 1), SnapshotPath: "/fixture/source"})
	if e != nil {
		t.Fatal(e)
	}
	model := contracts.ResolvedModel{ProviderID: "openai", ConnectionType: "openai", Model: "fixture", WireAPI: "responses", CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "fixture"}
	policy := contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
	m, _ := json.Marshal(model)
	p, _ := json.Marshal(policy)
	input := &contracts.ScanInput{SchemaVersion: 1, Knowledge: docs, ScanPrompt: contracts.Some("frozen prompt")}
	task := Task{ScanNo: "advanced-fixture", Provider: "openai", Model: "fixture", SourcePath: snap.SnapshotPath, SourceSnapshotID: snap.ID, SourceContentHash: snap.ContentHash, InputContractVersion: 2, ResultContractVersion: 1, ObservationContractVersion: 1, ScanMode: "standard", TimeoutMinutes: 1, ModelSnapshotJSON: string(m), PolicySnapshotJSON: string(p), ScanInput: input}
	id, e := s.InsertPending(ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveKnowledge(ctx, first.DocumentID, 1, "new", "changed"); e != nil {
		t.Fatal(e)
	}
	if e = s.ArchiveKnowledge(ctx, first.DocumentID, true); e != nil {
		t.Fatal(e)
	}
	limits := ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: t.TempDir()}
	caps := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
	results := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
	obs := contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
	if v, e := s.ClaimExecutionWithObservations(ctx, "old", limits, caps, results, obs); e != nil || v != nil {
		t.Fatal("old agent claimed advanced task", e, v)
	}
	if e = s.ReportAgentWithObservations(ctx, "old", 1, caps, results, obs); e != nil {
		t.Fatal(e)
	}
	overview, e := s.QueueOverview(ctx, limits, id)
	if e != nil || len(overview.Tasks) != 1 || overview.Tasks[0].Reason != "scan_input_incompatible" {
		t.Fatal("compatibility missing", e, overview.Tasks)
	}
	obs.JobVersions = append(obs.JobVersions, 4)
	v, e := s.ClaimExecutionWithObservations(ctx, "new", limits, caps, results, obs)
	if e != nil || v == nil || v.ID != id || v.ScanInput.Knowledge[0].Content != "immutable" {
		t.Fatal("frozen input lost", e, v)
	}
	task.ScanNo = "archived-reference"
	if _, e = s.InsertPending(ctx, task); !errors.Is(e, ErrKnowledgeConflict) {
		t.Fatal("new archived reference accepted", e)
	}
}

func TestKnowledgeRejectsOversizedUtf8AndConcurrentVersionOverwrite(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	if _, e := s.SaveKnowledge(ctx, "", 0, "too large", strings.Repeat("中", 22000)); !errors.Is(e, ErrKnowledgeInput) {
		t.Fatal("UTF-8 byte bound ignored", e)
	}
	v, e := s.SaveKnowledge(ctx, "", 0, "concurrent", "original")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, e := s.SaveKnowledge(ctx, v.DocumentID, 1, "concurrent", fmt.Sprint("writer-", i))
			results <- e
		}(i)
	}
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			success++
		} else if errors.Is(e, ErrKnowledgeConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("lost update", success, conflict)
	}
	history, e := s.ListKnowledge(ctx, "", v.DocumentID, false, 1)
	if e != nil || history.Total != 2 {
		t.Fatal("concurrent history", e, history.Total)
	}
}
