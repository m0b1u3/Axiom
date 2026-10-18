package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestComponentSourceCopyAndVersionGate(t *testing.T) {
	source := t.TempDir()
	file := filepath.Join(source, "fixture.ts")
	if e := os.WriteFile(file, []byte("Fixture only"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, _, _, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	copy, e := materializeComponentSource(t.Context(), source, t.TempDir(), hash)
	if e != nil || copy == source {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(copy, "fixture.ts"), []byte("private modification"), 0600); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(file)
	if e != nil || string(raw) != "Fixture only" {
		t.Fatal("shared snapshot was modified", e)
	}
	if _, e = materializeComponentSource(t.Context(), source, t.TempDir(), "wrong hash"); e == nil {
		t.Fatal("source hash not checked")
	}
	raw, e = os.ReadFile("../../../contracts/fixtures/v1/job-components.json")
	if e != nil {
		t.Fatal(e)
	}
	job, e := contracts.DecodeScannerJobV7(raw, nil)
	if e != nil || job.ComponentPlanning.Source.ContentHash != job.Source.ContentHash {
		t.Fatal(e)
	}
	for _, v := range []int{7, 8} {
		raw, e = os.ReadFile("../../../contracts/fixtures/v1/scanner-description-v" + string(rune('0'+v)) + ".json")
		if e != nil {
			t.Fatal(e)
		}
		caps, e := parseDescription(raw)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, jobVersion := range caps.Observation.JobVersions {
			if jobVersion == 7 {
				found = true
			}
		}
		if found != (v == 8) {
			t.Fatal("job 7 capability invented or missing", caps)
		}
	}
}

func TestComponentAuditDescriptionGate(t *testing.T) {
	for _, version := range []int{8, 9} {
		raw, e := os.ReadFile(fmt.Sprintf("../../../contracts/fixtures/v1/scanner-description-v%d.json", version))
		if e != nil {
			t.Fatal(e)
		}
		caps, e := parseDescription(raw)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, v := range caps.Observation.JobVersions {
			found = found || v == contracts.ComponentScanJobVersion
		}
		if found != (version == 9) {
			t.Fatal("component execution capability invented or missing", caps)
		}
	}
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/job-component-scan.json")
	if e != nil {
		t.Fatal(e)
	}
	job, e := contracts.DecodeScannerJobV8(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	job.ComponentScan.Execution.Attempt++
	changed, _ := contracts.EncodeJSON(job)
	if _, e = contracts.DecodeScannerJobV8(changed, nil); e == nil {
		t.Fatal("foreign parent/component execution accepted")
	}
}
