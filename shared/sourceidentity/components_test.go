package sourceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func componentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"src/api/a.ts", "src/web/b.ts", "src2/c.ts", ".ignored", "README.md", "源码/证据.ts", "\ue000.ts", "😀.ts", ".git/objects/fixture"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(file, []byte("Fixture only\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.MkdirAll(filepath.Join(root, "empty"), 0700); e != nil {
		t.Fatal(e)
	}
	return root
}
func TestComponentsInventoryAndBoundaries(t *testing.T) {
	root := componentFixture(t)
	all, e := CheckComponents(t.Context(), root, []Component{{"All", []string{"."}}})
	if e != nil || !all.Valid || all.FileCount != 8 {
		t.Fatal(e, all)
	}
	paths := []string{"src/api/a.ts", "src/web/b.ts", "src2/c.ts", ".ignored", "README.md", "源码/证据.ts", "\ue000.ts", "😀.ts"}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p + "\x00"))
	}
	if all.InventoryHash != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("inventory order changed")
	}
	partial, e := CheckComponents(t.Context(), root, []Component{{"Source", []string{"src"}}})
	if e != nil || partial.Valid || partial.CoveredFileCount != 2 || len(partial.UncoveredPaths) != 6 {
		t.Fatal(e, partial)
	}
	invalid, e := CheckComponents(t.Context(), root, []Component{{" Same ", []string{"src", "src/api"}}, {"Same", []string{"src/api/a.ts"}}, {"Empty", []string{"empty"}}})
	if e != nil || invalid.Valid || invalid.OverlapCount != 3 || !reflect.DeepEqual(invalid.EmptyComponents, []int{2}) || !reflect.DeepEqual(invalid.DuplicateNames, []string{"Same"}) {
		t.Fatal(e, invalid)
	}
	for _, p := range []string{"../secret", "/etc/passwd", "src/../README.md", "src\\api", ".git", "src/.git/objects", "missing", "C:drive"} {
		if _, e := CheckComponents(t.Context(), root, []Component{{"Bad", []string{p}}}); e == nil {
			t.Fatal("accepted invalid component path", p)
		}
	}
}
func TestComponentsLimitsAndSourceFaults(t *testing.T) {
	root := componentFixture(t)
	parts := []Component{}
	for i := 0; i < 25; i++ {
		parts = append(parts, Component{"same", []string{"."}})
	}
	v, e := CheckComponents(t.Context(), root, parts)
	if e != nil || v.OverlapCount != 300 || len(v.Overlaps) != 200 || v.Valid {
		t.Fatal(e, v)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e = CheckComponents(ctx, root, parts); e == nil {
		t.Fatal("cancellation ignored")
	}
	if e = os.Symlink(filepath.Join(root, "README.md"), filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e = CheckComponents(t.Context(), root, parts); e == nil {
		t.Fatal("symlink accepted")
	}
	empty := t.TempDir()
	v, e = CheckComponents(t.Context(), empty, []Component{{"Empty", []string{"."}}})
	if e != nil || v.Valid || len(v.EmptyComponents) != 1 {
		t.Fatal(e, v)
	}
}
