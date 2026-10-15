package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestQuarantineRecoveryAndBoundaries(t *testing.T) {
	if !CleanupSupported {
		t.Skip("certified Unix cleanup only")
	}
	root := t.TempDir()
	outside := t.TempDir()
	old := filepath.Join(root, "old")
	os.Mkdir(old, 0700)
	os.WriteFile(filepath.Join(old, "proof.txt"), []byte("fixture historical proof"), 0600)
	os.WriteFile(filepath.Join(outside, "retain"), []byte("outside"), 0600)
	size, hash, e := Usage(t.Context(), root, "old", true)
	if e != nil || size != 24 || len(hash) != 64 {
		t.Fatal(size, hash, e)
	}
	if _, e = Quarantine(t.Context(), root, "old", "job-0", "different"); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	if _, e = Quarantine(t.Context(), root, "../escape", "job-0", hash); !errors.Is(e, ErrBoundary) {
		t.Fatal(e)
	}
	if _, e = Quarantine(t.Context(), root, "old", "job-0", hash); e != nil {
		t.Fatal(e)
	}
	// Simulate restart after rename, before the phase ACK. New original data must
	// survive a resumed quarantine and purge of the old, approved destination.
	os.Mkdir(old, 0700)
	os.WriteFile(filepath.Join(old, "new.txt"), []byte("new fixture data"), 0600)
	if _, e = Quarantine(t.Context(), root, "old", "job-0", hash); e != nil {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if e = Purge(canceled, root, "job-0"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e = Purge(t.Context(), root, "job-0"); e != nil {
		t.Fatal(e)
	}
	if e = Purge(t.Context(), root, "job-0"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(old, "new.txt")); e != nil {
		t.Fatal("new source was deleted", e)
	}
	os.Symlink(outside, filepath.Join(root, "link"))
	if _, _, e = Usage(t.Context(), root, "link", true); !errors.Is(e, ErrBoundary) {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(outside, "retain")); e != nil {
		t.Fatal(e)
	}
}
func TestFingerprintSurvivesCopyAndRejectsContentChange(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, root := range []string{a, b} {
		os.Mkdir(filepath.Join(root, "entry"), 0700)
		os.WriteFile(filepath.Join(root, "entry", "file"), []byte("same"), 0600)
	}
	_, first, e := Usage(t.Context(), a, "entry", true)
	if e != nil {
		t.Fatal(e)
	}
	_, second, e := Usage(t.Context(), b, "entry", true)
	if e != nil || first != second {
		t.Fatal("backup copy changed fingerprint", e)
	}
	os.WriteFile(filepath.Join(b, "entry", "file"), []byte("diff"), 0600)
	_, changed, e := Usage(t.Context(), b, "entry", true)
	if e != nil || changed == first {
		t.Fatal("same-size mutation not detected", e)
	}
}
