package store_test

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"os"
	"path/filepath"
	"testing"
)

func TestInterimReviewSourceCitations(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "src"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "src/query.ts"), []byte("a\nb\nc\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, _, _, e := sourceidentity.TreeHash(t.Context(), root)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ report, state string }{
		{"Source `src/query.ts:2-3`.", "checked"}, {"Source `src/query.ts:2-3` and invented `src/db.ts:5-8`.", "blocked"}, {"Source `src/query.ts:2-9`.", "blocked"}, {"Source `../private.env:1`.", "blocked"}, {"Source `/etc/passwd:1`.", "blocked"}, {"Source only, no citations.", "no_references"}, {"Source src/query.ts:2-3.", "checked"}, {"Source `src/query.ts:9007199254740992`.", "blocked"},
	} {
		v := store.CheckInterimReviewSources(t.Context(), root, hash, tc.report)
		if string(v.State) != tc.state {
			t.Fatalf("%s: %+v", tc.report, v)
		}
		raw, _ := contracts.EncodeJSON(v)
		if e := contracts.ValidateJSON(contracts.KindInterimReviewSourceCheck, raw, nil); e != nil {
			t.Fatal(e)
		}
	}
	if v := store.CheckInterimReviewSources(t.Context(), root, "a"+hash[1:], "`src/query.ts:2`"); v.State != "unavailable" {
		t.Fatal("foreign source accepted", v)
	}
}
