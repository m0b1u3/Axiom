package evidence

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDK050PortableEvidenceKeepsCanonicalPointersAndOldReader(t *testing.T) {
	for _, version := range []string{"legacy", "050"} {
		t.Run(version, func(t *testing.T) {
			source, factsFile := "evidence-source.json", "evidence-facts.json"
			if version == "050" {
				source, factsFile = "evidence050-source.json", "evidence050-facts.json"
			}
			raw, err := os.ReadFile(filepath.Join("../../../contracts/fixtures/v1", source))
			if err != nil {
				t.Fatal(err)
			}
			var finding contracts.Finding
			if err = json.Unmarshal(raw, &finding); err != nil {
				t.Fatal(err)
			}
			raw, err = os.ReadFile(filepath.Join("../../../contracts/fixtures/v1", factsFile))
			if err != nil {
				t.Fatal(err)
			}
			facts, err := contracts.DecodeFindingEvidence(raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err = os.WriteFile(filepath.Join(dir, "fixture.ts"), []byte("// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n"), 0600); err != nil {
				t.Fatal(err)
			}
			hash, _, _, err := sourceidentity.TreeHash(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			digest := contracts.Digest(hash)
			facts.SourceContentHash = &digest
			raw, err = contracts.EncodeJSON(facts)
			if err != nil {
				t.Fatal(err)
			}
			finding.Extensions = contracts.Some(contracts.JSONObject{"axiomEvidence": raw})
			graph, err := Project(t.Context(), finding, facts.Execution, hash, strings.Repeat("a", 64), dir)
			if err != nil {
				t.Fatal(err)
			}
			observed := 0
			canonical := false
			for _, node := range graph.Nodes {
				if node.State == "verified" {
					t.Fatal("model validation was promoted to verification")
				}
				if node.Kind == "code_evidence" && node.State == "observed" {
					observed++
				}
				if node.SourcePointer == "/raw/attackPath/dataflow/outcome" {
					canonical = true
				}
			}
			if observed != 2 || (version == "050" && !canonical) {
				t.Fatal("versioned evidence or source bindings lost", observed, canonical)
			}
			// Corrupt source never upgrades a model claim; the archived graph remains readable.
			os.WriteFile(filepath.Join(dir, "fixture.ts"), []byte("// Fixtures / Mock changed revision\n"), 0600)
			graph, err = Project(t.Context(), finding, facts.Execution, hash, strings.Repeat("a", 64), dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range graph.Nodes {
				if node.State != "inferred" {
					t.Fatal("changed source became an observation")
				}
			}
		})
	}
}
