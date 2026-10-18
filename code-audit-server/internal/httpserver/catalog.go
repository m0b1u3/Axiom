package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Pagination is opt-in on the existing routes. Omitting both page parameters
// retains the historical array envelope for older Web/API consumers.
func catalogPageQuery(q url.Values, sorts []string) (store.PageOptions, bool, error) {
	p := store.PageOptions{Page: 1, PageSize: 50, Sort: q.Get("sort"), Order: q.Get("order")}
	paged := q.Has("page") || q.Has("pageSize")
	for _, key := range []string{"page", "pageSize", "sort", "order", "q", "severity", "status", "sourceSnapshotId", "sourcePath", "projectId", "usage", "id"} {
		if len(q[key]) > 1 {
			return p, paged, fmt.Errorf("查询参数 %s 不能重复", key)
		}
	}
	for key, dest := range map[string]*int{"page": &p.Page, "pageSize": &p.PageSize} {
		if q.Has(key) {
			n, err := strconv.Atoi(q.Get(key))
			if err != nil || n < 1 {
				return p, paged, fmt.Errorf("分页参数 %s 必须是正整数", key)
			}
			*dest = n
		}
	}
	if p.PageSize > 100 {
		return p, paged, fmt.Errorf("每页最多读取 100 条记录")
	}
	if p.Sort != "" && !catalogValue(p.Sort, sorts...) {
		return p, paged, fmt.Errorf("不支持的排序字段")
	}
	if !catalogValue(p.Order, "", "asc", "desc") {
		return p, paged, fmt.Errorf("排序方向必须为 asc 或 desc")
	}
	if !paged && (p.Sort != "" || p.Order != "") {
		return p, paged, fmt.Errorf("排序需要同时指定分页参数")
	}
	if !utf8.ValidString(q.Get("q")) || strings.ContainsRune(q.Get("q"), 0) || utf8.RuneCountInString(q.Get("q")) > 256 {
		return p, paged, fmt.Errorf("搜索词须为有效文本，最多 256 个字符")
	}
	if !utf8.ValidString(q.Get("sourcePath")) || strings.ContainsRune(q.Get("sourcePath"), 0) || utf8.RuneCountInString(q.Get("sourcePath")) > 4096 {
		return p, paged, fmt.Errorf("来源路径须为有效文本，最多 4096 个字符")
	}
	return p, paged, nil
}

func catalogValue(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func catalogID(q url.Values, key string) (*int64, error) {
	if !q.Has(key) {
		return nil, nil
	}
	n, err := strconv.ParseInt(q.Get(key), 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("查询参数 %s 必须是非负整数", key)
	}
	return &n, nil
}

func findingCatalogQuery(q url.Values) (store.FindingFilter, store.PageOptions, bool, error) {
	p, paged, err := catalogPageQuery(q, []string{"severity", "title", "filePath", "status", "lastSeenAt"})
	f := store.FindingFilter{Severity: q.Get("severity"), Status: q.Get("status"), Query: q.Get("q"), SourcePath: q.Get("sourcePath")}
	if err != nil {
		return f, p, paged, err
	}
	if !catalogValue(f.Severity, "", "critical", "high", "medium", "low", "info") {
		return f, p, paged, fmt.Errorf("不支持的漏洞严重度")
	}
	if f.Status != "" && !store.FindingStatuses[f.Status] {
		return f, p, paged, fmt.Errorf("不支持的漏洞处理状态")
	}
	if f.ProjectID, err = catalogID(q, "projectId"); err != nil {
		return f, p, paged, err
	}
	if id, e := catalogID(q, "sourceSnapshotId"); e != nil {
		return f, p, paged, e
	} else if id != nil {
		f.SourceSnapshotID = *id
	}
	if paged {
		f.Query = strings.TrimSpace(f.Query)
	}
	return f, p, paged, nil
}

func snapshotCatalogQuery(q url.Values) (store.SnapshotFilter, store.PageOptions, bool, error) {
	p, paged, err := catalogPageQuery(q, []string{"id", "originalFilename", "archiveSha256", "fileCount", "totalSize", "scanCount", "createdAt"})
	f := store.SnapshotFilter{Query: strings.TrimSpace(q.Get("q")), Usage: q.Get("usage")}
	if err != nil {
		return f, p, paged, err
	}
	if !catalogValue(f.Usage, "", "referenced", "unused") {
		return f, p, paged, fmt.Errorf("不支持的快照引用状态")
	}
	if f.ProjectID, err = catalogID(q, "projectId"); err != nil {
		return f, p, paged, err
	}
	if id, e := catalogID(q, "id"); e != nil {
		return f, p, paged, e
	} else if id != nil {
		f.ID = *id
	}
	return f, p, paged, nil
}
