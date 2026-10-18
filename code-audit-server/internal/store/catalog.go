package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

// Historical SQLite reads used Go's Unicode lowercase matching. Keep that
// behavior when moving filtering into SQL (SQLite's built-in LOWER is ASCII).
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("axiom_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] == nil {
			return "", nil
		}
		return strings.ToLower(args[0].(string)), nil
	})
}

type CatalogPage[T any] struct {
	Items    []T            `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Counts   map[string]int `json:"counts,omitempty"`
}

type PageOptions struct {
	Page     int
	PageSize int
	Sort     string
	Order    string
}

func (p PageOptions) valid() bool {
	return p.Page > 0 && p.PageSize > 0 && p.PageSize <= 100 && (p.Order == "" || p.Order == "asc" || p.Order == "desc")
}

func pageNumber(requested, total, size int) int {
	last := 1
	if total > 0 {
		last = (total-1)/size + 1
	}
	if requested > last {
		return last
	}
	return requested
}

func catalogOrder(p PageOptions, allowed map[string]string, fallback, direction string) (string, error) {
	key := p.Sort
	if key == "" {
		key = fallback
	}
	column, ok := allowed[key]
	if !ok {
		return "", fmt.Errorf("invalid sort")
	}
	if p.Order != "" {
		direction = p.Order
	}
	return column + " " + strings.ToUpper(direction) + ", id DESC", nil
}

func (s *Store) catalogSearch(columns []string, query string) (string, []any) {
	lower := "LOWER"
	if !s.db.postgres {
		lower = "axiom_lower"
	}
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(query)) + "%"
	parts := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, column := range columns {
		parts[i] = lower + "(" + column + ") LIKE ? ESCAPE '!'"
		args[i] = pattern
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// ProjectID nil means all projects; a non-nil zero means unassigned. The old
// ListSnapshots(projectID=0) all-project meaning is preserved by its wrapper.
type SnapshotFilter struct {
	Query     string
	ProjectID *int64
	Usage     string
	ID        int64
}

const snapshotCatalog = `WITH catalog AS (
 SELECT ss.id, ss.original_filename, ss.archive_sha256, ss.content_hash, ss.snapshot_path,
 ss.file_count, ss.total_size, COALESCE(ss.project_id,0) AS project_id,
 (SELECT COUNT(*) FROM scan_tasks st WHERE st.source_snapshot_id=ss.id) AS scan_count,
 ss.created_at, COALESCE(p.name,'') AS project_name,EXISTS(SELECT 1 FROM git_snapshots g WHERE g.snapshot_id=ss.id) AS git_diff
 FROM source_snapshots ss LEFT JOIN projects p ON p.id=ss.project_id
) `

const snapshotSelection = `id, original_filename, archive_sha256, content_hash, snapshot_path,
 file_count, total_size, project_id, scan_count, created_at,git_diff`

func (s *Store) snapshotConditions(f SnapshotFilter) (string, []any) {
	where := "1=1"
	args := []any{}
	if f.ProjectID != nil {
		where += " AND project_id=?"
		args = append(args, *f.ProjectID)
	}
	if f.ID > 0 {
		where += " AND id=?"
		args = append(args, f.ID)
	}
	if f.Usage == "referenced" {
		where += " AND scan_count>0"
	}
	if f.Usage == "unused" {
		where += " AND scan_count=0"
	}
	if f.Query != "" {
		query, values := s.catalogSearch([]string{"original_filename", "archive_sha256", "content_hash", "snapshot_path", "project_name"}, f.Query)
		where += " AND " + query
		args = append(args, values...)
	}
	return where, args
}

func readSnapshots(rows *sql.Rows) ([]SnapshotRow, error) {
	defer rows.Close()
	items := []SnapshotRow{}
	for rows.Next() {
		var item SnapshotRow
		if err := rows.Scan(&item.ID, &item.OriginalFilename, &item.ArchiveSha256, &item.ContentHash,
			&item.SnapshotPath, &item.FileCount, &item.TotalSize, &item.ProjectID, &item.ScanCount, &item.CreatedAt, &item.GitDiff); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListSnapshotsPage(ctx context.Context, f SnapshotFilter, p PageOptions) (*CatalogPage[SnapshotRow], error) {
	if !p.valid() || (f.Usage != "" && f.Usage != "referenced" && f.Usage != "unused") {
		return nil, fmt.Errorf("invalid snapshot query")
	}
	order, err := catalogOrder(p, map[string]string{"id": "id", "originalFilename": "original_filename", "archiveSha256": "archive_sha256", "fileCount": "file_count", "totalSize": "total_size", "scanCount": "scan_count", "createdAt": "created_at"}, "id", "desc")
	if err != nil {
		return nil, err
	}
	// Totals, clamped page and rows describe one database snapshot.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where, args := s.snapshotConditions(f)
	result := &CatalogPage[SnapshotRow]{PageSize: p.PageSize}
	if err = tx.QueryRowContext(ctx, snapshotCatalog+"SELECT COUNT(*) FROM catalog WHERE "+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.Page = pageNumber(p.Page, result.Total, p.PageSize)
	rows, err := tx.QueryContext(ctx, snapshotCatalog+"SELECT "+snapshotSelection+" FROM catalog WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", append(args, p.PageSize, (result.Page-1)*p.PageSize)...)
	if err != nil {
		return nil, err
	}
	if result.Items, err = readSnapshots(rows); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Build the global fingerprint projection BEFORE filtering. A filter must not
// resurrect an older occurrence or discard an earlier human disposition.
const findingCatalog = `WITH catalog AS (
 SELECT l.id, l.scan_id, l.fingerprint, l.title, l.severity, COALESCE(l.category,'') AS category,
 COALESCE(l.cwe,'') AS cwe, COALESCE(l.file_path,'') AS file_path, l.start_line, l.end_line, l.description,
 COALESCE((SELECT f2.status FROM findings f2 WHERE f2.fingerprint=l.fingerprint AND f2.status<>'open'
 ORDER BY f2.id DESC LIMIT 1), COALESCE(l.status,'open')) AS status,
 COALESCE(l.raw,'') AS raw, fs.scan_id AS first_seen_scan_id, fs.created_at AS first_seen_at, ls.created_at AS last_seen_at,
 COALESCE(ls.source_snapshot_id,0) AS source_snapshot_id, COALESCE(ls.source_path,'') AS source_path,
 COALESCE(ls.project_id,0) AS project_id
 FROM findings l JOIN scan_tasks ls ON ls.id=l.scan_id
 JOIN (SELECT f.fingerprint,MAX(f.id) AS mid FROM findings f JOIN scan_tasks t ON t.id=f.scan_id
 WHERE t.status IN ('completed','partial') GROUP BY f.fingerprint) mm ON mm.mid=l.id
 JOIN (SELECT f.fingerprint,f.scan_id,f.created_at FROM findings f
 JOIN (SELECT fingerprint,MIN(id) AS mn FROM findings GROUP BY fingerprint) g ON g.mn=f.id) fs ON fs.fingerprint=l.fingerprint
) `

const findingSelection = `id,scan_id,fingerprint,title,severity,category,cwe,file_path,start_line,end_line,description,status,raw,first_seen_scan_id,first_seen_at,last_seen_at`
const severityOrder = `CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END`

func (s *Store) findingConditions(f FindingFilter) (string, []any) {
	where := "1=1"
	args := []any{}
	if f.ProjectID != nil {
		where += " AND project_id=?"
		args = append(args, *f.ProjectID)
	}
	if f.SourceSnapshotID > 0 {
		where += " AND source_snapshot_id=?"
		args = append(args, f.SourceSnapshotID)
	}
	if f.SourcePath != "" {
		// Source-path matching was historically a case-sensitive substring.
		if s.db.postgres {
			where += " AND STRPOS(source_path,?)>0"
		} else {
			where += " AND INSTR(source_path,?)>0"
		}
		args = append(args, f.SourcePath)
	}
	if f.Status != "" {
		where += " AND status=?"
		args = append(args, f.Status)
	}
	if f.Severity != "" {
		where += " AND severity=?"
		args = append(args, f.Severity)
	}
	if f.Query != "" {
		query, values := s.catalogSearch([]string{"title", "file_path", "category", "fingerprint", "cwe"}, f.Query)
		where += " AND " + query
		args = append(args, values...)
	}
	return where, args
}

func readFindingSummaries(rows *sql.Rows) ([]FindingSummary, error) {
	defer rows.Close()
	items := []FindingSummary{}
	for rows.Next() {
		var item FindingSummary
		var cwe, raw string
		var start, end sql.NullInt64
		if err := rows.Scan(&item.ID, &item.ScanID, &item.Fingerprint, &item.Title, &item.Severity, &item.Category, &cwe, &item.FilePath,
			&start, &end, &item.Description, &item.Status, &raw, &item.FirstSeenScanID, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			return nil, err
		}
		item.CWE = CWEList(cwe)
		if start.Valid {
			n := int(start.Int64)
			item.StartLine = &n
		}
		if end.Valid {
			n := int(end.Int64)
			item.EndLine = &n
		}
		if raw != "" {
			item.Raw = json.RawMessage(raw)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListFindingSummariesPage(ctx context.Context, f FindingFilter, p PageOptions) (*CatalogPage[FindingSummary], error) {
	if !p.valid() {
		return nil, fmt.Errorf("invalid pagination")
	}
	order, err := catalogOrder(p, map[string]string{"severity": severityOrder, "title": "title", "filePath": "file_path", "status": "status", "lastSeenAt": "last_seen_at"}, "severity", "asc")
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Severity facets respect search, disposition and source/project scope, but
	// exclude the selected severity so users can compare all severities.
	facet := f
	facet.Severity = ""
	where, args := s.findingConditions(facet)
	rows, err := tx.QueryContext(ctx, findingCatalog+"SELECT severity,COUNT(*) FROM catalog WHERE "+where+" GROUP BY severity", args...)
	if err != nil {
		return nil, err
	}
	result := &CatalogPage[FindingSummary]{PageSize: p.PageSize, Counts: map[string]int{"": 0, "critical": 0, "high": 0, "medium": 0, "low": 0, "info": 0}}
	for rows.Next() {
		var severity string
		var n int
		if err = rows.Scan(&severity, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if severity != "" {
			result.Counts[severity] += n
		}
		result.Counts[""] += n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result.Total = result.Counts[f.Severity]
	result.Page = pageNumber(p.Page, result.Total, p.PageSize)
	where, args = s.findingConditions(f)
	rows, err = tx.QueryContext(ctx, findingCatalog+"SELECT "+findingSelection+" FROM catalog WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", append(args, p.PageSize, (result.Page-1)*p.PageSize)...)
	if err != nil {
		return nil, err
	}
	if result.Items, err = readFindingSummaries(rows); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
