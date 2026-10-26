package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Only platform execution metadata is selected. The HTTP projection reads the
// portable interim contract; Store does not depend on Scanner/SDK file formats.
type InterimExecution struct {
	TaskID            int64  `json:"taskId"`
	ScanNo            string `json:"scanNo"`
	Attempt           int    `json:"attempt"`
	Status            string `json:"status"`
	SourceContentHash string `json:"sourceContentHash"`
	SourceName        string `json:"sourceName"`
	ProjectID         int64  `json:"projectId"`
	ProjectName       string `json:"projectName"`
	StartedAt         string `json:"startedAt"`
	FinishedAt        string `json:"finishedAt"`
	OutputPath        string `json:"-"`
}
type InterimExecutionFilter struct {
	Query, Status string
	ProjectID     *int64
}

func (s *Store) ListInterimExecutionsPage(ctx context.Context, f InterimExecutionFilter, p PageOptions) (*CatalogPage[InterimExecution], error) {
	if !p.valid() {
		return nil, fmt.Errorf("invalid interim execution page")
	}
	out := &CatalogPage[InterimExecution]{Items: []InterimExecution{}, Page: 1, PageSize: p.PageSize}
	if !s.db.postgres {
		return out, nil
	} // Historical SQLite has no attempt journal.
	const catalog = `WITH catalog AS (
 SELECT a.task_id,st.scan_no,a.attempt,a.status,st.source_content_hash,
 COALESCE(ss.original_filename,'') AS source_name,COALESCE(st.project_id,0) AS project_id,
 COALESCE(p.name,'') AS project_name,a.started_at,COALESCE(a.finished_at,'') AS finished_at,a.output_path
 FROM scan_attempts a JOIN scan_tasks st ON st.id=a.task_id
 LEFT JOIN source_snapshots ss ON ss.id=st.source_snapshot_id LEFT JOIN projects p ON p.id=st.project_id
 WHERE st.scan_mode='standard' AND length(st.source_content_hash)=64
 AND NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=st.id)
 AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=st.id)
 AND NOT EXISTS(SELECT 1 FROM component_scans op WHERE op.task_id=st.id)
 ) `
	where, args := "1=1", []any{}
	if f.ProjectID != nil {
		where += " AND project_id=?"
		args = append(args, *f.ProjectID)
	}
	if f.Status != "" {
		where += " AND status=?"
		args = append(args, f.Status)
	}
	if f.Query != "" {
		q, v := s.catalogSearch([]string{"scan_no", "source_name", "project_name", "CAST(task_id AS TEXT)"}, f.Query)
		where += " AND " + q
		args = append(args, v...)
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, catalog+"SELECT COUNT(*) FROM catalog WHERE "+where, args...).Scan(&out.Total); e != nil {
		return nil, e
	}
	out.Page = pageNumber(p.Page, out.Total, p.PageSize)
	rows, e := tx.QueryContext(ctx, catalog+`SELECT task_id,scan_no,attempt,status,source_content_hash,source_name,project_id,project_name,started_at,finished_at,output_path FROM catalog WHERE `+where+` ORDER BY started_at DESC,task_id DESC,attempt DESC LIMIT ? OFFSET ?`, append(args, p.PageSize, (out.Page-1)*p.PageSize)...)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var v InterimExecution
		if e = rows.Scan(&v.TaskID, &v.ScanNo, &v.Attempt, &v.Status, &v.SourceContentHash, &v.SourceName, &v.ProjectID, &v.ProjectName, &v.StartedAt, &v.FinishedAt, &v.OutputPath); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}
