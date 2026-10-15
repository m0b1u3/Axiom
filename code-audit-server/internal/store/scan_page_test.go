package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestScanPage(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		_, err = s.InsertPending(ctx, Task{ScanNo: fmt.Sprintf("scan-%d", i), SourcePath: "/src", Provider: "openai", Model: "test", ScanMode: "standard"})
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.ListScansPage(ctx, 0, 2, 3, "", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 7 || len(p.Items) != 3 || p.Items[0].ScanNo != "scan-3" || p.Counts[""] != 7 {
		t.Fatalf("%+v", p)
	}
	p, err = s.ListScansPage(ctx, 0, 9, 3, "scan-6", "")
	if err != nil || p.Page != 1 || p.Total != 1 {
		t.Fatal(err, p)
	}
	p, err = s.ListScansPage(ctx, 0, 1, 3, "%", "")
	if err != nil || p.Total != 0 || p.Items == nil {
		t.Fatal(err, p)
	}
}
