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

func setup(t *testing.T) (contracts.Finding, contracts.ExecutionRef, string, string) {
	t.Helper()
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/evidence-source.json")
	if e != nil {
		t.Fatal(e)
	}
	var f contracts.Finding
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	if e = os.WriteFile(filepath.Join(root, "fixture.ts"), []byte("// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, _, _, e := sourceidentity.TreeHash(t.Context(), root)
	if e != nil {
		t.Fatal(e)
	}
	ref := contracts.ExecutionRef{TaskID: "9007199254740993", Attempt: 2}
	raw, e = os.ReadFile("../../../contracts/fixtures/v1/evidence-facts.json")
	if e != nil {
		t.Fatal(e)
	}
	facts, e := contracts.DecodeFindingEvidence(raw, &ref)
	if e != nil {
		t.Fatal(e)
	}
	digest := contracts.Digest(hash)
	facts.SourceContentHash = &digest
	raw, _ = contracts.EncodeJSON(facts)
	f.Extensions = contracts.Some(contracts.JSONObject{"axiomEvidence": raw})
	return f, ref, hash, root
}
func TestProjectionBindsSourceAndNeverPromotesModelValidation(t *testing.T) {
	f, ref, hash, root := setup(t)
	g, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root)
	if e != nil {
		t.Fatal(e)
	}
	observed := 0
	for _, n := range g.Nodes {
		if n.Kind == "code_evidence" {
			if n.State != "observed" || n.SourceCheck != "matched" {
				t.Fatal(n)
			}
			observed++
		}
		if n.Kind == "validation" && n.State != "inferred" {
			t.Fatal("SDK status became runtime proof")
		}
		if n.State == "verified" {
			t.Fatal("manufactured verification")
		}
	}
	if observed != 2 || g.Origin != "native_evidence" {
		t.Fatal(g)
	}
	next, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(g)
	b, _ := json.Marshal(next)
	if string(a) != string(b) {
		t.Fatal("projection not deterministic")
	}
}
func TestProjectionRejectsIdentityAndReferenceCorruption(t *testing.T) {
	for _, kind := range []string{"attempt", "fingerprint", "source", "pointer", "dangling", "verified", "code", "location", "reference"} {
		t.Run(kind, func(t *testing.T) {
			f, ref, hash, root := setup(t)
			var v map[string]any
			json.Unmarshal(f.Extensions.Value["axiomEvidence"], &v)
			switch kind {
			case "attempt":
				v["execution"].(map[string]any)["attempt"] = 3
			case "fingerprint":
				v["findingFingerprint"] = "other"
			case "source":
				v["sourceContentHash"] = strings.Repeat("b", 64)
			case "pointer":
				v["nodes"].([]any)[0].(map[string]any)["sourcePointer"] = "/raw/missing"
			case "dangling":
				v["edges"].([]any)[0].(map[string]any)["to"] = "missing"
			case "verified":
				v["nodes"].([]any)[0].(map[string]any)["state"] = "verified"
			case "code":
				v["nodes"].([]any)[1].(map[string]any)["code"] = "unrelated code"
			case "location":
				v["nodes"].([]any)[1].(map[string]any)["location"].(map[string]any)["startLine"] = 3
			case "reference":
				for _, raw := range v["edges"].([]any) {
					edge := raw.(map[string]any)
					if edge["kind"] == "supports" {
						edge["from"] = v["nodes"].([]any)[2].(map[string]any)["id"]
						break
					}
				}
			}
			raw, _ := json.Marshal(v)
			f.Extensions.Value["axiomEvidence"] = raw
			if _, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root); e == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
func TestMissingChangedAndNonMatchingSourceRemainInferred(t *testing.T) {
	for _, kind := range []string{"missing", "changed", "mismatch", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f, ref, hash, root := setup(t)
			switch kind {
			case "missing":
				os.RemoveAll(root)
			case "changed":
				os.WriteFile(filepath.Join(root, "fixture.ts"), []byte("changed"), 0600)
			case "symlink":
				os.Remove(filepath.Join(root, "fixture.ts"))
				os.Symlink("/etc/passwd", filepath.Join(root, "fixture.ts"))
			case "mismatch":
				var facts contracts.FindingEvidence
				json.Unmarshal(f.Extensions.Value["axiomEvidence"], &facts)
				text := "not present"
				facts.Nodes[1].Code = &text
				var source map[string]any
				json.Unmarshal(f.Raw.Value, &source)
				source["codeEvidence"].([]any)[0].(map[string]any)["code"] = text
				changed, _ := json.Marshal(source)
				f.Raw = contracts.Some(contracts.JSONValue(changed))
				raw, _ := contracts.EncodeJSON(facts)
				f.Extensions.Value["axiomEvidence"] = raw
			}
			g, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root)
			if e != nil {
				t.Fatal(e)
			}
			if g.Nodes[1].State != "inferred" || g.Completeness != "partial" {
				t.Fatal(g.Nodes[1], g.Completeness)
			}
		})
	}
}
func TestHistoricalProjectionDoesNotParseRawSDKOrRewriteIt(t *testing.T) {
	f, ref, hash, root := setup(t)
	f.Extensions = contracts.Optional[contracts.JSONObject]{}
	f.Source = contracts.Some("legacy source text")
	f.Sink = contracts.Some("legacy sink text")
	before, _ := contracts.EncodeJSON(f)
	g, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := contracts.EncodeJSON(f)
	if string(before) != string(after) || g.Origin != "legacy_projection" {
		t.Fatal("historical source changed")
	}
	for _, edge := range g.Edges {
		if edge.Kind != "contains" {
			t.Fatal("fabricated legacy relation")
		}
	}
}

func TestHistoricalMissingTitleAndEmptySnippetRemainReadable(t *testing.T) {
	f, ref, hash, root := setup(t)
	f.Extensions = contracts.Optional[contracts.JSONObject]{}
	f.Title = ""
	f.CodeEvidence = contracts.Some([]contracts.CodeEvidence{{Code: ""}})
	g, e := Project(t.Context(), f, ref, hash, strings.Repeat("a", 64), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Nodes) != 2 || g.Nodes[1].Code != nil || g.Origin != "legacy_projection" {
		t.Fatal(g)
	}
}
