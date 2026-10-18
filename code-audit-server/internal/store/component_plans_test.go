package store

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestComponentResultRecomputesEvidenceAndMatchesSDKNames(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "components.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	root := t.TempDir()
	file := filepath.Join(root, "fixture.ts")
	if e = os.WriteFile(file, []byte("Fixture only"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, n, size, e := sourceidentity.TreeHash(t.Context(), root)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), SnapshotRow{OriginalFilename: "fixture.zip", ArchiveSha256: hash, ContentHash: hash, SnapshotPath: root, FileCount: n, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	doc, e := contracts.DecodeComponentPlanDocument([]byte(`{"components":[{"name":"\ufeffComplete\ufeff","paths":["."]}]}`), nil)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := s.CreateComponentPlan(t.Context(), snap.ID, "Plan", doc, 1)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/job-components.json")
	if e != nil {
		t.Fatal(e)
	}
	job, e := contracts.DecodeScannerJobV7(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	op := job.ComponentPlanning
	op.PlanID = plan.ID
	op.Source = plan.Source
	op.Document = &doc
	model, _ := contracts.EncodeJSON(job.Model)
	policy, _ := contracts.EncodeJSON(job.Policy)
	id, e := s.InsertPending(t.Context(), Task{ScanNo: "component-result-test", SourcePath: root, SourceSnapshotID: snap.ID, SourceContentHash: hash, Provider: string(job.Model.ProviderID), Model: job.Model.Model, BaseURL: job.Model.BaseURL.Value, TimeoutMinutes: int(job.Policy.TimeBudget.ScanSeconds / 60), ScanMode: "standard", InputContractVersion: 5, ResultContractVersion: 1, ObservationContractVersion: 1, ModelSnapshotJSON: string(model), PolicySnapshotJSON: string(policy), BudgetAccounting: true, ScanInput: &job.ScanInput, ComponentPlanning: &op})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Get(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	op = *task.ComponentPlanning
	op.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}
	resultRaw, e := os.ReadFile("../../../contracts/fixtures/v1/component-result.json")
	if e != nil {
		t.Fatal(e)
	}
	out, e := contracts.DecodeComponentPlanningResult(resultRaw, nil)
	if e != nil {
		t.Fatal(e)
	}
	out.Execution = op.Execution
	out.Operation = op
	normalized := normalizedComponentDocument(doc)
	if normalized.Components[0].Name != "Complete" {
		t.Fatal("Go does not match SDK ECMAScript trim")
	}
	out.Document = &normalized
	coverage, e := sourceidentity.CheckComponents(t.Context(), root, []sourceidentity.Component{{Name: "Complete", Paths: []string{"."}}})
	if e != nil {
		t.Fatal(e)
	}
	coverageRaw, _ := json.Marshal(coverage)
	checked, e := contracts.DecodeComponentPlanCoverage(coverageRaw, nil)
	if e != nil {
		t.Fatal(e)
	}
	out.Coverage = &checked
	full := resultFixture(t)
	full.Execution = op.Execution
	full.Findings = []contracts.Finding{}
	zero := float64(0)
	full.Cost.Value.EstimatedUSD = &zero
	completion := contracts.ExecutionCompletion{SchemaVersion: 1, Execution: op.Execution, Result: contracts.Some(full)}
	validate := func(v contracts.ComponentPlanningResult) error {
		raw, _ := contracts.EncodeJSON(v)
		full.Extensions = contracts.Some(contracts.JSONObject{"componentPlanning": raw})
		completion.Result = contracts.Some(full)
		return s.validateComponentCompletion(t.Context(), id, completion)
	}
	if e = validate(out); e != nil {
		t.Fatal("valid component result rejected", e)
	}
	for _, kind := range []string{"inventory", "source", "revision", "document", "coverage"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := contracts.EncodeJSON(out)
			v, _ := contracts.DecodeComponentPlanningResult(raw, nil)
			switch kind {
			case "inventory":
				v.Coverage.InventoryHash = contracts.Digest(hash)
			case "source":
				v.Operation.Source.ContentHash = contracts.Digest("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
			case "revision":
				v.Operation.Revision++
			case "document":
				v.Document.Components[0].Name = "Forged"
			case "coverage":
				v.Coverage.FileCount = 2
				v.Coverage.CoveredFileCount = 2
			}
			if e = validate(v); e == nil {
				t.Fatal("forged result accepted", kind)
			}
		})
	}
	t.Run("immutable-version-integrity-and-row-binding", func(t *testing.T) {
		raw, e := os.ReadFile("../../../contracts/fixtures/v1/component-version.json")
		if e != nil {
			t.Fatal(e)
		}
		v, e := contracts.DecodeComponentPlanVersion(raw, nil)
		if e != nil {
			t.Fatal(e)
		}
		v.PlanID = plan.ID
		v.Source = plan.Source
		v.Document = normalized
		v.Coverage = checked
		v.SHA256 = ""
		v.SHA256 = contracts.Digest(componentDigest(v))
		planID := mustComponentID(plan.ID)
		if _, e = s.db.ExecContext(t.Context(), `INSERT INTO component_plan_versions(plan_id,version,revision,document_json) VALUES(?,1,1,?)`, planID, componentJSON(v)); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ComponentVersion(t.Context(), planID, 1); e != nil {
			t.Fatal(e)
		}
		if page, e := s.ComponentVersions(t.Context(), planID, 1); e != nil || len(page.Items) != 1 {
			t.Fatal(e, page)
		}
		for _, mutation := range []string{"checksum", "plan", "version", "revision"} {
			changed := v
			switch mutation {
			case "checksum":
				changed.ConfirmedBy = "999"
			case "plan":
				changed.PlanID = "999"
			case "version":
				changed.Version = 9
			case "revision":
				changed.Revision = 9
			}
			if mutation != "checksum" {
				changed.SHA256 = ""
				changed.SHA256 = contracts.Digest(componentDigest(changed))
			}
			if _, e = s.db.ExecContext(t.Context(), `UPDATE component_plan_versions SET document_json=? WHERE plan_id=?`, componentJSON(changed), planID); e != nil {
				t.Fatal(e)
			}
			if _, e = s.ComponentVersion(t.Context(), planID, 1); e == nil {
				t.Fatal("wrong immutable version accepted", mutation)
			}
			if _, e = s.ComponentVersions(t.Context(), planID, 1); e == nil {
				t.Fatal("wrong version listed", mutation)
			}
		}
	})
	if e = os.WriteFile(file, []byte("tamper"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = validate(out); e == nil {
		t.Fatal("changed source accepted")
	}
}
