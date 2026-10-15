package store

import (
	"context"
	"fmt"
	"strings"
)

type ScanPage struct {
	Items    []Task         `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Counts   map[string]int `json:"counts"`
}

func (s *Store) ListScansPage(ctx context.Context, projectID int64, page, size int, keyword, status string) (*ScanPage, error) {
	if page < 1 || size < 1 || size > 100 {
		return nil, fmt.Errorf("invalid pagination")
	}
	where := "1=1"
	args := []any{}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		where += " AND (LOWER(t.scan_no) LIKE ? ESCAPE '!' OR LOWER(t.source_path) LIKE ? ESCAPE '!' OR LOWER(t.model) LIKE ? ESCAPE '!')"
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword)) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	countWhere := where
	countArgs := append([]any{}, args...)
	if projectID > 0 {
		countWhere += " AND t.project_id=?"
		countArgs = append(countArgs, projectID)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT t.status, COUNT(*) FROM scan_tasks t WHERE "+countWhere+" GROUP BY t.status", countArgs...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"": 0, "pending": 0, "running": 0, "done": 0, "failed": 0}
	for rows.Next() {
		var state string
		var n int
		if err = rows.Scan(&state, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[""] += n
		switch state {
		case "claimed", "scanning", "cancel_requested":
			counts["running"] += n
		case "completed", "partial":
			counts["done"] += n
		case "failed", "timeout":
			counts["failed"] += n
		default:
			counts[state] += n
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	switch status {
	case "":
	case "running":
		where += " AND t.status IN ('claimed','scanning','cancel_requested')"
	case "done":
		where += " AND t.status IN ('completed','partial')"
	case "failed":
		where += " AND t.status IN ('failed','timeout')"
	default:
		where += " AND t.status=?"
		args = append(args, status)
	}
	total := counts[status]
	last := (total + size - 1) / size
	if last < 1 {
		last = 1
	}
	if page > last {
		page = last
	}
	items, err := s.listScansQuery(ctx, projectID, where, args, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Task{}
	}
	// List views do not consume full progress, audit instructions or diagnostics.
	for i := range items {
		items[i].ProgressJSON = ""
		items[i].BusinessContext = ""
		items[i].ErrorMessage = ""
	}
	return &ScanPage{Items: items, Total: total, Page: page, PageSize: size, Counts: counts}, nil
}
