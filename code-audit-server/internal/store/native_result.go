package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed native_result.sql
var nativeResultSQL string

func (s *Store) indexNativeResult(ctx context.Context, tx *transaction, c Completion) error {
	if c.contract == nil || !c.contract.Result.Present {
		return nil
	}
	native, err := contracts.NativeSDKResult(c.contract.Result.Value)
	// Unsupported optional metadata stays in the full archive, without a guessed index.
	if err != nil || native == nil || native.ScanID == nil || *native.ScanID == "" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sdk_native_scans(task_id,attempt,sdk_scan_id,source_hash) SELECT id,?,?,COALESCE(source_content_hash,'') FROM scan_tasks WHERE id=? ON CONFLICT(task_id,attempt) DO NOTHING`, c.Attempt, *native.ScanID, c.TaskID)
	return err
}

type SDKExecutionLink struct {
	Execution contracts.ExecutionRef `json:"execution"`
	ScanNo    string                 `json:"scanNo"`
	Status    string                 `json:"status"`
}
type SDKExecutionLinks struct {
	Items    []SDKExecutionLink `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
	ScanID   string             `json:"scanId"`
}

// One native ID can have several archived attempts. Return every candidate,
// constrained to the immutable source, instead of guessing a Web task identity.
func (s *Store) SDKExecutionLinks(ctx context.Context, sdkID, sourceHash string, page int) (SDKExecutionLinks, error) {
	out := SDKExecutionLinks{Items: []SDKExecutionLink{}, PageSize: 50, ScanID: sdkID}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sdk_native_scans WHERE sdk_scan_id=? AND source_hash=?`, sdkID, sourceHash).Scan(&out.Total); e != nil {
		return out, e
	}
	out.Page = pageNumber(page, out.Total, out.PageSize)
	rows, e := tx.QueryContext(ctx, `SELECT n.task_id,n.attempt,t.scan_no,a.status FROM sdk_native_scans n JOIN scan_tasks t ON t.id=n.task_id JOIN scan_attempts a ON a.task_id=n.task_id AND a.attempt=n.attempt WHERE n.sdk_scan_id=? AND n.source_hash=? ORDER BY n.task_id DESC,n.attempt DESC LIMIT ? OFFSET ?`, sdkID, sourceHash, out.PageSize, (out.Page-1)*out.PageSize)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v SDKExecutionLink
		var taskID int64
		if e = rows.Scan(&taskID, &v.Execution.Attempt, &v.ScanNo, &v.Status); e != nil {
			return out, e
		}
		v.Execution.TaskID = contracts.TaskID(fmt.Sprint(taskID))
		out.Items = append(out.Items, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	return out, tx.Commit()
}
