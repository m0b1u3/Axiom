package scanner

import (
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedResult(t *testing.T) {
	rich, err := os.ReadFile("../../../contracts/fixtures/v1/result-rich.json")
	if err != nil {
		t.Fatal(err)
	}
	expected := contracts.ExecutionRef{TaskID: "9007199254740993", Attempt: 2}
	for _, scenario := range []string{"valid", "empty", "partial-empty", "missing-commit", "bad-json", "hash", "size", "marker-identity", "result-identity", "version", "severity", "symlink", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "normalized"), 0700); err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(rich, &object); err != nil {
				t.Fatal(err)
			}
			if scenario == "empty" || scenario == "partial-empty" {
				object["findings"] = []any{}
			}
			if scenario == "partial-empty" {
				object["status"] = "partial"
				object["coverage"].(map[string]any)["completeness"] = "partial"
			}
			if scenario == "result-identity" {
				object["execution"].(map[string]any)["attempt"] = 3
			}
			if scenario == "version" {
				object["schemaVersion"] = 3
			}
			if scenario == "severity" {
				object["findings"].([]any)[0].(map[string]any)["severity"] = "urgent"
			}
			data, _ := json.Marshal(object)
			if scenario == "bad-json" {
				data = []byte("{")
			}
			hash := sha256.Sum256(data)
			marker := contracts.ResultCommit{SchemaVersion: 1, Execution: expected, ResultSchemaVersion: 1, RelativePath: LayoutNormalized, SizeBytes: int64(len(data)), SHA256: contracts.Digest(hex.EncodeToString(hash[:]))}
			if scenario == "hash" {
				marker.SHA256 = contracts.Digest(hex.EncodeToString(make([]byte, 32)))
			}
			if scenario == "size" {
				marker.SizeBytes++
			}
			if scenario == "marker-identity" {
				marker.Execution.Attempt++
			}
			if err := os.WriteFile(filepath.Join(dir, LayoutNormalized), data, 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				outside := filepath.Join(t.TempDir(), "result.json")
				os.WriteFile(outside, data, 0600)
				os.Remove(filepath.Join(dir, LayoutNormalized))
				if e := os.Symlink(outside, filepath.Join(dir, LayoutNormalized)); e != nil {
					t.Skip(e)
				}
			}
			if scenario == "oversized" {
				if e := os.Truncate(filepath.Join(dir, LayoutNormalized), contracts.MaxDocumentBytes+1); e != nil {
					t.Fatal(e)
				}
			}
			if scenario != "missing-commit" {
				raw, _ := json.Marshal(marker)
				if err := os.WriteFile(filepath.Join(dir, LayoutCommit), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, e := ReadCommitted(dir, expected)
			if scenario == "valid" || scenario == "empty" || scenario == "partial-empty" {
				if e != nil || got == nil {
					t.Fatal(e)
				}
				if got.Execution != expected || !got.Cost.Present || got.Cost.Value.EstimatedUSD == nil || *got.Cost.Value.EstimatedUSD != 0 {
					t.Fatal("identity or observed zero cost changed")
				}
				if scenario == "valid" && !got.Findings[0].CodeEvidence.Present {
					t.Fatal("code evidence lost")
				}
			} else if e == nil || got != nil {
				t.Fatal("uncommitted, foreign or corrupt result accepted")
			}
		})
	}
}
