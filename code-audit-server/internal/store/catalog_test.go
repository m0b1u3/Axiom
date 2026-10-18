package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogSQLite(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "historical.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	testCatalog(t, s)
}
func TestCatalogPostgres(t *testing.T) { testCatalog(t, pgStore(t)) }

func testCatalog(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	user, err := s.EnsureUser(ctx, "fixture-catalog", "fixture-only-hash")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, "Fixture 中文 École 项目", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var oldest *SnapshotRow
	for i := 0; i < 237; i++ {
		v := SnapshotRow{OriginalFilename: fmt.Sprintf("fixture-%03d.zip", i), ArchiveSha256: fmt.Sprintf("sha-%03d", i), ContentHash: fmt.Sprintf("content-%03d", i), SnapshotPath: fmt.Sprintf("/fixtures/%03d", i), FileCount: i + 1, TotalSize: int64(i + 1)}
		if i == 0 {
			v.OriginalFilename = "Fixture 中文 ÉCOLE 100%_!.zip"
			v.ProjectID = project.ID
		}
		item, e := s.InsertSnapshot(ctx, v)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			oldest = item
		}
	}
	zero := int64(0)
	for _, tc := range []struct {
		f    SnapshotFilter
		want int
	}{
		{SnapshotFilter{}, 237}, {SnapshotFilter{Query: "100%_!"}, 1}, {SnapshotFilter{Query: "中文 école"}, 1},
		{SnapshotFilter{Query: "content-000"}, 1}, {SnapshotFilter{Query: "/fixtures/000"}, 1},
		{SnapshotFilter{ProjectID: &zero}, 236}, {SnapshotFilter{ProjectID: &project.ID}, 1}, {SnapshotFilter{Query: "%NOT_LITERAL_"}, 0},
	} {
		got, e := s.ListSnapshotsPage(ctx, tc.f, PageOptions{Page: math.MaxInt, PageSize: 50})
		if e != nil || got.Total != tc.want {
			t.Fatalf("snapshots %+v: %+v %v", tc.f, got, e)
		}
		if got.Items == nil {
			t.Fatal("empty items must serialize as []")
		}
	}
	seen := map[int64]bool{}
	for page := 1; page <= 5; page++ {
		got, e := s.ListSnapshotsPage(ctx, SnapshotFilter{}, PageOptions{Page: page, PageSize: 50, Sort: "fileCount", Order: "asc"})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range got.Items {
			if seen[v.ID] {
				t.Fatal("overlapping pages")
			}
			seen[v.ID] = true
		}
	}
	if len(seen) != 237 || !seen[oldest.ID] {
		t.Fatal("page traversal lost historical snapshots")
	}
	legacy, err := s.ListSnapshots(ctx, 200, 0)
	if err != nil || len(legacy) != 200 {
		t.Fatal("legacy snapshot array changed", err)
	}
	first, err := s.InsertPending(ctx, Task{ScanNo: "fixture-first", SourcePath: "/First/Path", SourceSnapshotID: oldest.ID, ProjectID: project.ID, Provider: "custom", Model: "fixture", ScanMode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	seed := []Finding{}
	for i := 0; i < 357; i++ {
		v := Finding{Fingerprint: fmt.Sprintf("fp-%03d", i), Title: fmt.Sprintf("Fixture finding %03d", i), Severity: "high", Description: "fixture"}
		if i == 0 {
			v.Severity = "low"
			v.Title = "Fixture 中文 ÉCOLE 100%_!"
			v.Category = "historical-category"
			v.CWE = []string{"CWE-9999"}
			v.FilePath = "src/legacy.ts"
			v.Raw = json.RawMessage(`{"fixtureOnly":true,"evidence":[{"code":"historical raw"}]}`)
		}
		seed = append(seed, v)
	}
	if err = s.ReplaceFindings(ctx, first, seed); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, first, "completed", "", "", len(seed)); err != nil {
		t.Fatal(err)
	}
	oldFindings, err := s.ListFindings(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	oldID := int64(0)
	for _, v := range oldFindings {
		if v.Fingerprint == "fp-000" {
			oldID = v.ID
		}
	}
	if _, err = s.SetFindingStatus(ctx, oldID, "confirmed"); err != nil {
		t.Fatal(err)
	}
	latest, err := s.InsertPending(ctx, Task{ScanNo: "fixture-latest", SourcePath: "/Latest/Path", Provider: "custom", Model: "fixture", ScanMode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceFindings(ctx, latest, []Finding{seed[0]}); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, latest, "partial", "", "", 1); err != nil {
		t.Fatal(err)
	}
	failed, err := s.InsertPending(ctx, Task{ScanNo: "fixture-failed", SourcePath: "/Ignore", Provider: "custom", Model: "fixture", ScanMode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	ignored := seed[0]
	ignored.Title = "must not replace terminal occurrence"
	if err = s.ReplaceFindings(ctx, failed, []Finding{ignored, {Fingerprint: "failed-only", Title: "must not appear", Severity: "critical", Description: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, failed, "failed", "fixture", "fixture", 2); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		f    FindingFilter
		want int
	}{
		{FindingFilter{}, 357}, {FindingFilter{Severity: "low"}, 1}, {FindingFilter{Status: "confirmed"}, 1},
		{FindingFilter{Query: "100%_!"}, 1}, {FindingFilter{Query: "中文 école"}, 1}, {FindingFilter{Query: "cwe-9999"}, 1},
		{FindingFilter{Query: "historical-category"}, 1}, {FindingFilter{Query: "fp-000"}, 1}, {FindingFilter{Query: "src/legacy.ts"}, 1},
		{FindingFilter{Query: "must not"}, 0}, {FindingFilter{ProjectID: &project.ID}, 356}, {FindingFilter{ProjectID: &zero}, 1},
		{FindingFilter{SourceSnapshotID: oldest.ID}, 356}, {FindingFilter{SourcePath: "Latest"}, 1}, {FindingFilter{SourcePath: "latest"}, 0},
		{FindingFilter{Severity: "high", Status: "confirmed"}, 0},
	} {
		got, e := s.ListFindingSummariesPage(ctx, tc.f, PageOptions{Page: math.MaxInt, PageSize: 50})
		if e != nil || got.Total != tc.want {
			t.Fatalf("findings %+v: %+v %v", tc.f, got, e)
		}
		if got.Items == nil {
			t.Fatal("empty finding items must serialize as []")
		}
	}
	got, err := s.ListFindingSummariesPage(ctx, FindingFilter{Severity: "low"}, PageOptions{Page: 1, PageSize: 50})
	if err != nil || got.Counts[""] != 357 || got.Counts["high"] != 356 || got.Total != 1 {
		t.Fatal("facets applied selected severity", got, err)
	}
	v := got.Items[0]
	if v.ScanID != latest || v.FirstSeenScanID != first || v.Status != "confirmed" || !strings.Contains(string(v.Raw), "historical raw") {
		t.Fatalf("historical projection changed: %+v", v)
	}
	// The low-severity record was outside the old unfiltered LIMIT 300. Filtering
	// the legacy array must now happen before its retained cap as well.
	oldPool, err := s.ListFindingSummaries(ctx, FindingFilter{Query: "100%_!"})
	if err != nil || len(oldPool) != 1 || oldPool[0].ID != v.ID {
		t.Fatal("legacy filtered pool lost old record", err)
	}
	seen = map[int64]bool{}
	for page := 1; page <= 8; page++ {
		got, e := s.ListFindingSummariesPage(ctx, FindingFilter{}, PageOptions{Page: page, PageSize: 50})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range got.Items {
			if seen[v.ID] {
				t.Fatal("overlapping finding pages")
			}
			seen[v.ID] = true
		}
	}
	if len(seen) != 357 {
		t.Fatal("page traversal lost fingerprints")
	}
	used, err := s.ListSnapshotsPage(ctx, SnapshotFilter{Usage: "referenced"}, PageOptions{Page: 1, PageSize: 50})
	if err != nil || used.Total != 1 || used.Items[0].ScanCount != 1 {
		t.Fatal("snapshot reference filter", used, err)
	}
	unused, err := s.ListSnapshotsPage(ctx, SnapshotFilter{Usage: "unused"}, PageOptions{Page: 99, PageSize: 100})
	if err != nil || unused.Total != 236 || unused.Page != 3 {
		t.Fatal("snapshot unused filter", unused, err)
	}
	if err = s.DeleteSnapshot(ctx, unused.Items[len(unused.Items)-1].ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListSnapshotsPage(ctx, SnapshotFilter{Usage: "unused"}, PageOptions{Page: 99, PageSize: 100})
	if err != nil || after.Total != 235 {
		t.Fatal("delete not reflected", after, err)
	}
	if _, err = s.ListSnapshotsPage(ctx, SnapshotFilter{}, PageOptions{Page: 1, PageSize: 50, Sort: "id; DROP TABLE findings"}); err == nil {
		t.Fatal("untrusted sort accepted")
	}
	if _, err = s.ListFindingSummariesPage(ctx, FindingFilter{}, PageOptions{Page: 1, PageSize: 101}); err == nil {
		t.Fatal("unbounded page accepted")
	}
	// Imported legacy records can contain an unknown/empty severity. They are
	// still readable and counted once in the all-severity facet.
	seed[0].Severity = ""
	if err = s.ReplaceFindings(ctx, latest, []Finding{seed[0]}); err != nil {
		t.Fatal(err)
	}
	unknown, err := s.ListFindingSummariesPage(ctx, FindingFilter{}, PageOptions{Page: 1, PageSize: 50})
	if err != nil || unknown.Total != 357 || unknown.Counts[""] != 357 {
		t.Fatal("unknown legacy severity double counted", unknown, err)
	}
}
