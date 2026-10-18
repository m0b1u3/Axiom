package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed sdk_operations.sql
var sdkOperationsSQL string

func containsOperationJob(c contracts.ObservationCapabilities) bool {
	for _, v := range c.JobVersions {
		if v == contracts.SDKOperationJobVersion {
			return true
		}
	}
	return false
}
func (s *Store) loadSDKOperation(ctx context.Context, t *Task) error {
	var raw string
	var origin sql.NullInt64
	e := s.db.QueryRowContext(ctx, `SELECT document_json,origin_task_id FROM sdk_operations WHERE task_id=?`, t.ID).Scan(&raw, &origin)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	v, e := contracts.DecodeSDKOperation([]byte(raw), nil)
	if e != nil {
		return e
	}
	v.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(t.ID)), Attempt: int64(max(t.Attempt, 1))}
	t.SDKOperation = &v
	t.SDKOperationKind = string(v.Kind)
	t.SDKOperationOrigin = origin.Int64
	return nil
}

func (s *Store) validateOperationCompletion(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM sdk_operations WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		if full.Result.Present {
			if _, ok := full.Result.Value.Extensions.Value["sdkOperation"]; ok {
				return ErrInvalidResult
			}
		}
		return nil
	}
	if e != nil {
		return e
	}
	spec, e := contracts.DecodeSDKOperation([]byte(raw), nil)
	if e != nil {
		return ErrInvalidResult
	}
	if !full.Result.Present {
		return nil
	} // killed before commit: execution still archives unknown cost
	result := full.Result.Value
	if len(result.Findings) != 0 {
		return ErrInvalidResult
	}
	data, ok := result.Extensions.Value["sdkOperation"]
	if !ok {
		return ErrInvalidResult
	}
	op, e := contracts.DecodeSDKOperationResult(data, &full.Execution)
	if e != nil || op.Kind != contracts.SDKOperationResultKind(spec.Kind) {
		return ErrInvalidResult
	}
	if spec.Kind == contracts.SDKOperationKindValidate && ((result.Cost.Present && result.Cost.Value != nil) || op.Cost.State != "unknown") {
		return ErrInvalidResult
	}
	if spec.Kind == contracts.SDKOperationKindPreflight && (!result.Cost.Present || result.Cost.Value == nil || result.Cost.Value.EstimatedUSD == nil || *result.Cost.Value.EstimatedUSD != 0) {
		return ErrInvalidResult
	}
	if (op.Error == nil) != (result.Status == contracts.TerminalStatusCompleted) {
		return ErrInvalidResult
	}
	return nil
}

type SDKOperationPage struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

func (s *Store) SDKOperations(ctx context.Context, origin int64, page int) (SDKOperationPage, error) {
	out := SDKOperationPage{Items: []Task{}, PageSize: 50}
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sdk_operations WHERE origin_task_id=?`, origin).Scan(&out.Total); e != nil {
		return out, e
	}
	out.Page = pageNumber(page, out.Total, out.PageSize)
	rows, e := s.db.QueryContext(ctx, `SELECT task_id FROM sdk_operations WHERE origin_task_id=? ORDER BY task_id DESC LIMIT ? OFFSET ?`, origin, out.PageSize, (out.Page-1)*out.PageSize)
	if e != nil {
		return out, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}

	for _, id := range ids {
		v, e := s.Get(ctx, id)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

func OperationResult(result contracts.ScanResult) (*contracts.SDKOperationResult, error) {
	if !result.Extensions.Present {
		return nil, nil
	}
	raw, ok := result.Extensions.Value["sdkOperation"]
	if !ok {
		return nil, nil
	}
	v, e := contracts.DecodeSDKOperationResult(json.RawMessage(raw), &result.Execution)
	return &v, e
}

func (s *Store) FrozenFinding(ctx context.Context, task int64, id string) (*contracts.JSONObject, error) {
	var raw, normalized string
	if e := s.db.QueryRowContext(ctx, `SELECT COALESCE(raw,''),COALESCE(normalized_json,'') FROM findings WHERE id=? AND scan_id=?`, id, task).Scan(&raw, &normalized); e != nil {
		return nil, e
	}
	var value contracts.JSONObject
	for _, candidate := range []string{raw, normalized} {
		if len(candidate) <= 8*1024*1024 && json.Unmarshal([]byte(candidate), &value) == nil && value != nil {
			return &value, nil
		}
	}
	return nil, errors.New("完整漏洞证据不可用，不能启动独立复核")
}

func (s *Store) verifyOperationKnowledge(ctx context.Context, tx *transaction, t Task) error {
	if t.SDKOperation != nil && t.SDKOperationOrigin > 0 && t.SDKOperation.Kind == contracts.SDKOperationKindPreflight {
		return nil
	} // compared to the immutable origin inside this transaction
	return s.verifyKnowledgeReferences(ctx, tx, *t.ScanInput)
}
