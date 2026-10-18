package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

//go:embed component_scans.sql
var componentScansSQL string

func (s *Store) saveComponentScan(ctx context.Context, tx *transaction, t Task, id int64) error {
	if t.ComponentScan == nil {
		return nil
	}
	r := *t.ComponentScan
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}
	var frozen string
	if e := tx.QueryRowContext(ctx, `SELECT document_json FROM component_plan_versions WHERE plan_id=? AND version=?`, r.PlanVersion.PlanID, r.PlanVersion.Version).Scan(&frozen); e != nil {
		return e
	}
	var v contracts.ComponentPlanVersion
	if json.Unmarshal([]byte(frozen), &v) != nil || !sameJSON(v, r.PlanVersion) || string(v.Source.SnapshotID) != fmt.Sprint(t.SourceSnapshotID) || string(v.Source.ContentHash) != t.SourceContentHash {
		return ErrComponentConflict
	}
	raw := componentJSON(r)
	if _, e := contracts.DecodeComponentScanRequest([]byte(raw), &r.Execution); e != nil {
		return e
	}
	if e := verifyComponentEvidence(ctx, t.SourcePath, t.SourceContentHash, v.Document, v.Coverage); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO component_scans(task_id,plan_id,version,workers,document_json) VALUES(?,?,?,?,?)`, id, v.PlanID, v.Version, min(r.Workers, int64(len(r.SelectedIndices))), raw)
	return e
}
func (s *Store) loadComponentScan(ctx context.Context, t *Task) error {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM component_scans WHERE task_id=?`, t.ID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	// Pending retries use their new selection; archived attempts retain their own request.
	if t.Status != "pending" && t.Attempt > 0 {
		var bound string
		e = s.db.QueryRowContext(ctx, `SELECT request_json FROM component_scan_attempts WHERE task_id=? AND attempt=?`, t.ID, t.Attempt).Scan(&bound)
		if e != nil {
			return e
		}
		raw = bound
	}
	r, e := contracts.DecodeComponentScanRequest([]byte(raw), nil)
	if e != nil {
		return e
	}
	t.ComponentScan = &r
	return nil
}
func (s *Store) reserveComponentScan(ctx context.Context, tx *transaction, id int64, attempt int, grant *int64) error {
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT document_json FROM component_scans WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if grant == nil {
		return ErrBudgetPolicy
	}
	r, e := contracts.DecodeComponentScanRequest([]byte(raw), nil)
	if e != nil {
		return e
	}
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt)}
	rounded, e := moneyMicros(dollars(*grant), true)
	if e != nil || rounded < 1 {
		return ErrBudgetPolicy
	}
	*grant = rounded
	r.GrantMicros = *grant
	if _, e = contracts.DecodeComponentScanRequest([]byte(componentJSON(r)), &r.Execution); e != nil {
		return ErrBudgetExhausted
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO component_scan_attempts(task_id,attempt,request_json) VALUES(?,?,?)`, id, attempt, componentJSON(r))
	if e != nil {
		return e
	}
	initial := initialComponentSnapshot(r)
	return s.recordComponentSnapshot(ctx, tx, id, int64(attempt), &initial)
}
func checkComponentSnapshot(request contracts.ComponentScanRequest, v contracts.ComponentScanSnapshot) error {
	if v.Execution != request.Execution || v.PlanID != request.PlanVersion.PlanID || v.Version != request.PlanVersion.Version || v.VersionSHA256 != request.PlanVersion.SHA256 || !sameJSON(v.Source, request.PlanVersion.Source) || v.GrantMicros != request.GrantMicros || len(v.Components) != len(request.SelectedIndices) {
		return ErrInvalidProgress
	}
	a := contracts.ComponentAllocations(request)
	for i, r := range v.Components {
		if r.Index != request.SelectedIndices[i] || r.AllocatedMicros != a[i] {
			return ErrInvalidProgress
		}
	}
	return nil
}
func (s *Store) recordComponentSnapshot(ctx context.Context, tx *transaction, id int64, attempt int64, v *contracts.ComponentScanSnapshot) error {
	var requestJSON string
	e := tx.QueryRowContext(ctx, `SELECT request_json FROM component_scan_attempts WHERE task_id=? AND attempt=?`, id, attempt).Scan(&requestJSON)
	if errors.Is(e, sql.ErrNoRows) {
		if v != nil {
			return ErrInvalidProgress
		}
		return nil
	}
	if e != nil {
		return e
	}
	if v == nil {
		return nil
	}
	r, e := contracts.DecodeComponentScanRequest([]byte(requestJSON), nil)
	if e != nil {
		return e
	}
	if e = checkComponentSnapshot(r, *v); e != nil {
		return e
	}
	var previous *string
	if e = tx.QueryRowContext(ctx, `SELECT snapshot_json FROM component_scan_attempts WHERE task_id=? AND attempt=?`, id, attempt).Scan(&previous); e != nil {
		return e
	}
	if previous != nil {
		old, e := contracts.DecodeComponentScanSnapshot([]byte(*previous), &v.Execution)
		if e != nil {
			return e
		}
		if old.ObservedMicros > v.ObservedMicros {
			return ErrInvalidProgress
		}
		for i, p := range old.Components {
			n := v.Components[i]
			if p.ObservedMicros != nil && (n.ObservedMicros == nil || *n.ObservedMicros < *p.ObservedMicros) {
				return ErrInvalidProgress
			}
			if p.Status != "pending" && p.Status != "running" && !reflect.DeepEqual(p, n) {
				return ErrInvalidProgress
			}
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE component_scan_attempts SET snapshot_json=? WHERE task_id=? AND attempt=?`, componentJSON(v), id, attempt); e != nil {
		return e
	}
	for _, row := range v.Components {
		if _, e = tx.ExecContext(ctx, `INSERT INTO component_scan_receipts(task_id,attempt,component_index,document_json) VALUES(?,?,?,?) ON CONFLICT(task_id,attempt,component_index) DO UPDATE SET document_json=excluded.document_json`, id, attempt, row.Index, componentJSON(row)); e != nil {
			return e
		}
	}
	// A component observation is itself fee evidence. Do not depend on a second
	// untyped cost callback (or a final heartbeat) to protect its lower bound.
	if _, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET observed_micros=CASE WHEN observed_micros IS NULL OR observed_micros<? THEN ? ELSE observed_micros END,booked_micros=CASE WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,source='sdk_progress' WHERE task_id=? AND attempt=? AND state='active'`, v.ObservedMicros, v.ObservedMicros, v.ObservedMicros, v.ObservedMicros, id, attempt); e != nil {
		return e
	}
	return nil
}

func (s *Store) componentParentStatus(ctx context.Context, tx *transaction, c Completion) (string, error) {
	if c.Status != "completed" || c.contract == nil || !c.contract.Result.Present {
		return c.Status, nil
	}
	raw := c.contract.Result.Value.Extensions.Value["componentScan"]
	if raw == nil {
		return c.Status, nil
	}
	result, e := contracts.DecodeComponentScanResult(raw, &c.contract.Execution)
	if e != nil {
		return "", e
	}
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT ON (component_index) document_json FROM component_scan_receipts WHERE task_id=? ORDER BY component_index,attempt DESC`, c.TaskID)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	count := 0
	status := c.Status
	for rows.Next() {
		var body string
		if e = rows.Scan(&body); e != nil {
			return "", e
		}
		r, e := contracts.DecodeComponentScanReceipt([]byte(body), nil)
		if e != nil {
			return "", e
		}
		count++
		if r.Status != "completed" {
			status = "partial"
		}
	}
	if count != len(result.Request.PlanVersion.Document.Components) {
		status = "partial"
	}
	return status, rows.Err()
}
func componentProgress(p *contracts.ProgressSnapshotV2) (*contracts.ComponentScanSnapshot, error) {
	if p == nil || !p.Extensions.Present {
		return nil, nil
	}
	raw := p.Extensions.Value["componentScan"]
	if raw == nil {
		return nil, nil
	}
	v, e := contracts.DecodeComponentScanSnapshot(raw, &p.Execution)
	return &v, e
}
func (s *Store) validateComponentScanCompletion(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT request_json FROM component_scan_attempts WHERE task_id=? AND attempt=?`, id, full.Execution.Attempt).Scan(&raw)
	data := json.RawMessage(nil)
	if full.Result.Present && full.Result.Value.Extensions.Present {
		data = full.Result.Value.Extensions.Value["componentScan"]
	}
	if errors.Is(e, sql.ErrNoRows) {
		if data != nil {
			return ErrInvalidResult
		}
		return nil
	}
	if e != nil {
		return e
	}
	if !full.Result.Present {
		return nil
	}
	r, e := contracts.DecodeComponentScanRequest([]byte(raw), &full.Execution)
	if e != nil {
		return ErrInvalidResult
	}
	out, e := contracts.DecodeComponentScanResult(data, &full.Execution)
	if e != nil || !sameJSON(out.Request, r) {
		return ErrInvalidResult
	}
	result := full.Result.Value
	if result.Error.Present {
		if out.Error == nil || !sameJSON(*out.Error, result.Error.Value) {
			return ErrInvalidResult
		}
	} else if out.Error != nil {
		return ErrInvalidResult
	}
	if result.Status == "completed" {
		if out.Snapshot.Matching.Status == "incomplete" {
			return ErrInvalidResult
		}
		for _, row := range out.Snapshot.Components {
			if row.Status != "completed" {
				return ErrInvalidResult
			}
		}
	}
	artifacts := map[string]contracts.ArtifactRef{}
	for _, a := range result.Artifacts {
		artifacts[string(a.RelativePath)] = a
	}
	rows := map[int64]contracts.ComponentScanReceipt{}
	var total int64
	allKnown := out.Snapshot.Matching.CostState == "known"
	for _, row := range out.Snapshot.Components {
		rows[row.Index] = row
		if row.FinalMicros == nil {
			allKnown = false
		} else {
			total += *row.ObservedMicros
		}
		if row.ResultArtifact != nil && !sameJSON(artifacts[string(row.ResultArtifact.RelativePath)], *row.ResultArtifact) {
			return ErrInvalidResult
		}
	}
	if allKnown {
		if !result.Cost.Present || result.Cost.Value == nil || result.Cost.Value.EstimatedUSD == nil {
			return ErrInvalidResult
		}
		n, e := moneyMicros(*result.Cost.Value.EstimatedUSD, true)
		if e != nil || n != total {
			return ErrInvalidResult
		}
	} else if result.Cost.Present && result.Cost.Value != nil {
		return ErrInvalidResult
	}
	counts := map[int64]int64{}
	for _, f := range result.Findings {
		if !f.Extensions.Present {
			return ErrInvalidResult
		}
		binding, e := contracts.DecodeComponentFindingBinding(f.Extensions.Value["axiomComponent"], &full.Execution)
		if e != nil {
			return ErrInvalidResult
		}
		row, ok := rows[binding.ComponentIndex]
		if !ok || binding.PlanID != r.PlanVersion.PlanID || binding.Version != r.PlanVersion.Version || binding.VersionSHA256 != r.PlanVersion.SHA256 || !sameJSON(binding.Source, r.PlanVersion.Source) || row.ScanID == nil || binding.ScanID != *row.ScanID || row.ResultArtifact == nil || !sameJSON(binding.ResultArtifact, *row.ResultArtifact) {
			return ErrInvalidResult
		}
		counts[row.Index]++
	}
	for _, row := range rows {
		if counts[row.Index] != row.FindingCount {
			return ErrInvalidResult
		}
	}
	var path string
	if e = s.db.QueryRowContext(ctx, `SELECT snapshot_path FROM source_snapshots WHERE id=?`, r.PlanVersion.Source.SnapshotID).Scan(&path); e != nil {
		return e
	}
	if e = verifyComponentEvidence(ctx, path, string(r.PlanVersion.Source.ContentHash), r.PlanVersion.Document, r.PlanVersion.Coverage); e != nil {
		return ErrInvalidResult
	}
	return nil
}
func (s *Store) prepareComponentRetry(ctx context.Context, tx *transaction, id int64, attempt int, indices []int64, raw string) error {
	r, e := contracts.DecodeComponentScanRequest([]byte(raw), nil)
	if e != nil {
		return e
	}
	// The most recent receipt for each component determines retry eligibility.
	rows, e := tx.QueryContext(ctx, `SELECT document_json FROM component_scan_receipts WHERE task_id=? AND attempt<=? ORDER BY attempt DESC`, id, attempt)
	if e != nil {
		return e
	}
	latest := map[int64]contracts.ComponentScanReceipt{}
	for rows.Next() {
		var body string
		if e = rows.Scan(&body); e != nil {
			rows.Close()
			return e
		}
		v, e := contracts.DecodeComponentScanReceipt([]byte(body), nil)
		if e != nil {
			rows.Close()
			return e
		}
		if _, ok := latest[v.Index]; !ok {
			latest[v.Index] = v
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for i, index := range indices {
		if index < 0 || index >= int64(len(r.PlanVersion.Document.Components)) || (i > 0 && index <= indices[i-1]) {
			return ErrComponentInvalid
		}
		if row, ok := latest[index]; ok && row.Status == "completed" {
			return fmt.Errorf("%w：已完成组件不能从失败重试入口重复执行", ErrComponentConflict)
		}
	}
	r.SelectedIndices = indices
	r.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt + 1)}
	if _, e = contracts.DecodeComponentScanRequest([]byte(componentJSON(r)), &r.Execution); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE component_scans SET document_json=?,workers=? WHERE task_id=?`, componentJSON(r), min(r.Workers, int64(len(r.SelectedIndices))), id)
	return e
}

type ComponentAttemptView struct {
	Request  contracts.ComponentScanRequest   `json:"request"`
	Snapshot *contracts.ComponentScanSnapshot `json:"snapshot"`
	Status   string                           `json:"status"`
}

func (s *Store) ComponentAttempts(ctx context.Context, id int64) ([]ComponentAttemptView, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT c.request_json,c.snapshot_json,a.status FROM component_scan_attempts c JOIN scan_attempts a ON a.task_id=c.task_id AND a.attempt=c.attempt WHERE c.task_id=? ORDER BY c.attempt DESC`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComponentAttemptView{}
	for rows.Next() {
		var r string
		var state *string
		var v ComponentAttemptView
		if e = rows.Scan(&r, &state, &v.Status); e != nil {
			return nil, e
		}
		v.Request, e = contracts.DecodeComponentScanRequest([]byte(r), nil)
		if e != nil {
			return nil, e
		}
		if state != nil {
			p, e := contracts.DecodeComponentScanSnapshot([]byte(*state), &v.Request.Execution)
			if e != nil {
				return nil, e
			}
			v.Snapshot = &p
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func initialComponentSnapshot(r contracts.ComponentScanRequest) contracts.ComponentScanSnapshot {
	v := contracts.ComponentScanSnapshot{SchemaVersion: 1, Execution: r.Execution, PlanID: r.PlanVersion.PlanID, Version: r.PlanVersion.Version, VersionSHA256: r.PlanVersion.SHA256, Source: r.PlanVersion.Source, Phase: "preparing", ObservedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339)), GrantMicros: r.GrantMicros, Components: []contracts.ComponentScanReceipt{}, Matching: contracts.ComponentScanMatching{Status: "not_started", CostState: "known", Reason: "尚未进入匹配阶段"}}
	for i, n := range contracts.ComponentAllocations(r) {
		v.Components = append(v.Components, contracts.ComponentScanReceipt{Index: r.SelectedIndices[i], Status: "pending", AllocatedMicros: n})
	}
	v.ReservedMicros = contracts.ComponentReserved(v.Components)
	return v
}
