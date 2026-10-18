package httpserver

import (
	"net/url"
	"strings"
	"testing"
)

func TestCatalogQueryBoundaries(t *testing.T) {
	for _, query := range []string{"page=0", "page=-1", "page=NaN", "page=1&q=%00", "page=99999999999999999999999999999", "pageSize=101", "pageSize=0", "page=1&page=2", "page=1&q=a&q=b", "page=1&order=DROP", "sort=id", "page=1&projectId=-1", "page=1&projectId=no", "page=1&projectId=", "page=1&sourceSnapshotId=-1", "page=1&q=" + url.QueryEscape(strings.Repeat("界", 257))} {
		q, _ := url.ParseQuery(query)
		// Source snapshot scope belongs only to findings.
		if _, _, _, err := findingCatalogQuery(q); err == nil {
			t.Errorf("finding query accepted %s", query)
		}
		if !strings.Contains(query, "sourceSnapshotId") {
			if _, _, _, err := snapshotCatalogQuery(q); err == nil {
				t.Errorf("snapshot query accepted %s", query)
			}
		}
	}
	for _, query := range []string{"page=1&severity=unknown", "page=1&status=resolved", "page=1&sort=scanId", "page=1&sourcePath=" + strings.Repeat("x", 4097)} {
		q, _ := url.ParseQuery(query)
		if _, _, _, err := findingCatalogQuery(q); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
	for _, query := range []string{"page=1&usage=all", "page=1&sort=project", "page=1&id=-1"} {
		q, _ := url.ParseQuery(query)
		if _, _, _, err := snapshotCatalogQuery(q); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
	q, _ := url.ParseQuery("pageSize=100&projectId=0&q=%E4%B8%AD%E6%96%87&usage=unused")
	f, p, paged, err := snapshotCatalogQuery(q)
	if err != nil || !paged || p.Page != 1 || p.PageSize != 100 || f.ProjectID == nil || *f.ProjectID != 0 || f.Query != "中文" {
		t.Fatal(f, p, paged, err)
	}
	_, _, paged, err = findingCatalogQuery(url.Values{})
	if err != nil || paged {
		t.Fatal("legacy array unexpectedly opted into pagination", err)
	}
}
