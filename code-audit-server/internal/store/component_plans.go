package store

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

//go:embed component_plans.sql
var componentPlansSQL string
var ErrComponentConflict = errors.New("组件计划已变化、操作尚未完成或校验不适用于当前草稿，请刷新后重试")
var ErrComponentInvalid = errors.New("组件计划不能冻结：需要当前草稿的成功路径校验，且没有重叠、空组件、重名或遗漏文件")

type ComponentPlanView = contracts.ComponentPlanView
type ComponentVersionItem struct {
	Version     int64  `json:"version"`
	Revision    int64  `json:"revision"`
	SHA256      string `json:"sha256"`
	ConfirmedAt string `json:"confirmedAt"`
	ConfirmedBy string `json:"confirmedBy"`
}
type ComponentVersionPage struct {
	Items    []ComponentVersionItem `json:"items"`
	Total    int                    `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

func utcComponentTime() string   { return time.Now().UTC().Format(time.RFC3339Nano) }
func componentJSON(v any) string { raw, _ := contracts.EncodeJSON(v); return string(raw) }
func componentDigest(v any) string {
	raw, _ := contracts.EncodeJSON(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (s *Store) GetComponentPlan(ctx context.Context, id int64) (contracts.ComponentPlanSnapshot, error) {
	var v contracts.ComponentPlanSnapshot
	var source, document, origin string
	e := s.db.QueryRowContext(ctx, `SELECT title,revision,source_json,document_json,origin,created_at,updated_at FROM component_plans WHERE id=?`, id).Scan(&v.Title, &v.Revision, &source, &document, &origin, &v.CreatedAt, &v.UpdatedAt)
	if e != nil {
		return v, e
	}
	v.SchemaVersion = 1
	v.ID = contracts.TaskID(fmt.Sprint(id))
	v.Origin = contracts.ComponentPlanSnapshotOrigin(origin)
	if e = json.Unmarshal([]byte(source), &v.Source); e != nil {
		return v, e
	}
	if e = json.Unmarshal([]byte(document), &v.Document); e != nil {
		return v, e
	}
	checked, e := contracts.DecodeComponentPlanSnapshot([]byte(componentJSON(v)), nil)
	return checked, e
}
func (s *Store) CreateComponentPlan(ctx context.Context, snapshot int64, title string, document contracts.ComponentPlanDocument, actor int64) (contracts.ComponentPlanSnapshot, error) {
	var empty contracts.ComponentPlanSnapshot
	if len([]rune(strings.TrimSpace(title))) < 1 || len([]rune(title)) > 200 {
		return empty, ErrComponentInvalid
	}
	if _, e := contracts.DecodeComponentPlanDocument([]byte(componentJSON(document)), nil); e != nil {
		return empty, e
	}
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return empty, e
	}
	defer release()
	snap, e := s.GetSnapshot(ctx, snapshot)
	if e != nil {
		return empty, e
	}
	hash, _, _, e := sourceidentity.TreeHash(ctx, snap.SnapshotPath)
	if e != nil || hash != snap.ContentHash {
		return empty, errors.New("源码快照缺失或内容已变化，不能创建组件计划")
	}
	source := contracts.ComponentSource{SnapshotID: contracts.TaskID(fmt.Sprint(snapshot)), ContentHash: contracts.Digest(hash)}
	if git, e := s.GitSnapshotTarget(ctx, snapshot); e != nil {
		return empty, e
	} else if git != nil {
		source.HeadCommit = &git.HeadCommit
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	now := utcComponentTime()
	var id int64
	// The storage gate plus the FK protects the source through plan creation.
	e = tx.QueryRowContext(ctx, `INSERT INTO component_plans(snapshot_id,source_json,title,revision,document_json,origin,created_at,updated_at) VALUES(?,?,?,1,?,'manual',?,?) RETURNING id`, snapshot, componentJSON(source), strings.TrimSpace(title), componentJSON(document), now, now).Scan(&id)
	if e != nil {
		return empty, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO component_plan_edits(plan_id,revision,document_json,origin,actor_id,created_at) VALUES(?,1,?,'manual',?,?)`, id, componentJSON(document), actor, now); e != nil {
		return empty, e
	}
	if e = tx.Commit(); e != nil {
		return empty, e
	}
	return s.GetComponentPlan(ctx, id)
}

func (s *Store) SaveComponentDraft(ctx context.Context, id, revision int64, document contracts.ComponentPlanDocument, actor int64, origin string) (contracts.ComponentPlanSnapshot, error) {
	var empty contracts.ComponentPlanSnapshot
	if origin != "manual" && origin != "sdk_suggested" {
		return empty, ErrComponentInvalid
	}
	if _, e := contracts.DecodeComponentPlanDocument([]byte(componentJSON(document)), nil); e != nil {
		return empty, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	var current int64
	query := `SELECT revision FROM component_plans WHERE id=?`
	if s.db.postgres {
		query += ` FOR UPDATE`
	}
	if e = tx.QueryRowContext(ctx, query, id).Scan(&current); e != nil {
		return empty, e
	}
	if current != revision {
		return empty, ErrComponentConflict
	}
	now := utcComponentTime()
	if _, e = tx.ExecContext(ctx, `UPDATE component_plans SET revision=revision+1,document_json=?,origin=?,updated_at=? WHERE id=?`, componentJSON(document), origin, now, id); e != nil {
		return empty, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO component_plan_edits(plan_id,revision,document_json,origin,actor_id,created_at) VALUES(?,?,?,?,?,?)`, id, revision+1, componentJSON(document), origin, actor, now); e != nil {
		return empty, e
	}
	if e = tx.Commit(); e != nil {
		return empty, e
	}
	return s.GetComponentPlan(ctx, id)
}

func (s *Store) ComponentPlanIDs(ctx context.Context, snapshot int64, page int) ([]int64, int, int, error) {
	ids := []int64{}
	var total int
	e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM component_plans WHERE snapshot_id=?`, snapshot).Scan(&total)
	if e != nil {
		return ids, 0, 1, e
	}
	page = pageNumber(page, total, 50)
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM component_plans WHERE snapshot_id=? ORDER BY id DESC LIMIT 50 OFFSET ?`, snapshot, (page-1)*50)
	if e != nil {
		return ids, total, page, e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return ids, total, page, e
		}
		ids = append(ids, id)
	}
	return ids, total, page, rows.Err()
}
func (s *Store) ComponentView(ctx context.Context, id int64) (ComponentPlanView, error) {
	p, e := s.GetComponentPlan(ctx, id)
	v := ComponentPlanView{Plan: p}
	if e != nil {
		return v, e
	}
	var task int64
	e = s.db.QueryRowContext(ctx, `SELECT task_id FROM component_operations WHERE plan_id=? ORDER BY task_id DESC LIMIT 1`, id).Scan(&task)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	key := fmt.Sprint(task)
	taskKey := contracts.TaskID(key)
	v.OperationTaskID = &taskKey
	bound, e := s.Get(ctx, task)
	if e != nil {
		return v, e
	}
	v.Operation = bound.ComponentPlanning
	if bound.ErrorMessage != "" {
		message := bound.ErrorMessage
		if len([]rune(message)) > 16000 {
			message = string([]rune(message)[:16000])
		}
		v.OperationError = &message
	}
	result, status, e := s.ComponentOperationResult(ctx, task)
	if e != nil {
		return v, e
	}
	v.OperationStatus = status
	v.Result = result
	if result != nil {
		hash := componentDigest(result)
		digest := contracts.Digest(hash)
		v.ResultSHA256 = &digest
	}
	return v, nil
}
func (s *Store) ComponentOperationResult(ctx context.Context, task int64) (*contracts.ComponentPlanningResult, string, error) {
	var raw, status string
	e := s.db.QueryRowContext(ctx, `SELECT t.status,COALESCE(a.result_json,'') FROM scan_tasks t JOIN component_operations op ON op.task_id=t.id LEFT JOIN scan_attempts a ON a.task_id=t.id AND a.attempt=t.attempt WHERE t.id=?`, task).Scan(&status, &raw)
	if e != nil {
		return nil, status, e
	}
	if raw == "" {
		return nil, status, nil
	}
	full, e := contracts.DecodeExecutionCompletion([]byte(raw), nil)
	if e != nil {
		return nil, status, e
	}
	if !full.Result.Present {
		return nil, status, nil
	}
	body := full.Result.Value.Extensions.Value["componentPlanning"]
	if body == nil {
		return nil, status, ErrInvalidResult
	}
	v, e := contracts.DecodeComponentPlanningResult(body, &full.Execution)
	return &v, status, e
}
func (s *Store) loadComponentOperation(ctx context.Context, t *Task) error {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM component_operations WHERE task_id=?`, t.ID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	v, e := contracts.DecodeComponentPlanningOperation([]byte(raw), nil)
	if e != nil {
		return e
	}
	v.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(t.ID)), Attempt: int64(max(t.Attempt, 1))}
	t.ComponentPlanning = &v
	t.SDKOperationKind = "component_" + string(v.Kind)
	return nil
}
func (s *Store) saveComponentOperation(ctx context.Context, tx *transaction, t Task, id int64) error {
	if t.ComponentPlanning == nil {
		return nil
	}
	op := *t.ComponentPlanning
	var revision, snapshot int64
	var source, document string
	query := `SELECT revision,snapshot_id,source_json,document_json FROM component_plans WHERE id=?`
	if s.db.postgres {
		query += ` FOR UPDATE`
	}
	if e := tx.QueryRowContext(ctx, query, string(op.PlanID)).Scan(&revision, &snapshot, &source, &document); e != nil {
		return e
	}
	var bound contracts.ComponentSource
	if json.Unmarshal([]byte(source), &bound) != nil || !sameJSON(bound, op.Source) || snapshot != t.SourceSnapshotID || string(op.Source.ContentHash) != t.SourceContentHash || revision != op.Revision {
		return ErrComponentConflict
	}
	if op.Kind == "normalize" && componentJSON(op.Document) != document {
		return ErrComponentConflict
	}
	var active int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM component_operations o JOIN scan_tasks t ON t.id=o.task_id WHERE o.plan_id=? AND o.revision=? AND t.status IN ('pending','claimed','scanning','cancel_requested')`, string(op.PlanID), op.Revision).Scan(&active); e != nil {
		return e
	}
	if active > 0 {
		return ErrComponentConflict
	}
	op.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}
	raw := componentJSON(op)
	if _, e := contracts.DecodeComponentPlanningOperation([]byte(raw), &op.Execution); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO component_operations(task_id,plan_id,revision,kind,document_json) VALUES(?,?,?,?,?)`, id, string(op.PlanID), op.Revision, op.Kind, raw)
	return e
}
func (s *Store) validateComponentCompletion(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM component_operations WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		if full.Result.Present && full.Result.Value.Extensions.Present && full.Result.Value.Extensions.Value["componentPlanning"] != nil {
			return ErrInvalidResult
		}
		return nil
	}
	if e != nil {
		return e
	}
	op, e := contracts.DecodeComponentPlanningOperation([]byte(raw), nil)
	if e != nil {
		return ErrInvalidResult
	}
	op.Execution = full.Execution
	if !full.Result.Present {
		return nil
	}
	result := full.Result.Value
	if len(result.Findings) != 0 {
		return ErrInvalidResult
	}
	data := result.Extensions.Value["componentPlanning"]
	out, e := contracts.DecodeComponentPlanningResult(data, &full.Execution)
	if e != nil || !sameJSON(op, out.Operation) || (out.Error == nil) != (result.Status == "completed") {
		return ErrInvalidResult
	}
	if op.Kind == "normalize" {
		if !result.Cost.Present || result.Cost.Value == nil || result.Cost.Value.EstimatedUSD == nil || *result.Cost.Value.EstimatedUSD != 0 {
			return ErrInvalidResult
		}
	} else if result.Cost.Present && result.Cost.Value != nil {
		return ErrInvalidResult
	}
	if out.Error != nil {
		return nil
	}
	if op.Kind == "normalize" && !sameJSON(normalizedComponentDocument(*op.Document), *out.Document) {
		return ErrInvalidResult
	}
	var path, hash string
	if e = s.db.QueryRowContext(ctx, `SELECT snapshot_path,content_hash FROM source_snapshots WHERE id=?`, string(op.Source.SnapshotID)).Scan(&path, &hash); e != nil {
		return e
	}
	if hash != string(op.Source.ContentHash) {
		return ErrInvalidResult
	}
	return verifyComponentEvidence(ctx, path, hash, *out.Document, *out.Coverage)
}
func normalizedComponentDocument(in contracts.ComponentPlanDocument) contracts.ComponentPlanDocument {
	raw := componentJSON(in)
	var out contracts.ComponentPlanDocument
	_ = json.Unmarshal([]byte(raw), &out)
	for i := range out.Components {
		out.Components[i].Name = sourceidentity.TrimComponentName(out.Components[i].Name)
		seen := map[contracts.ComponentPath]bool{}
		paths := []contracts.ComponentPath{}
		for _, p := range out.Components[i].Paths {
			if !seen[p] {
				paths = append(paths, p)
				seen[p] = true
			}
		}
		out.Components[i].Paths = paths
	}
	return out
}
func verifyComponentEvidence(ctx context.Context, path, hash string, doc contracts.ComponentPlanDocument, expected contracts.ComponentPlanCoverage) error {
	actual, _, _, e := sourceidentity.TreeHash(ctx, path)
	if e != nil || actual != hash {
		return ErrComponentInvalid
	}
	var components []sourceidentity.Component
	if json.Unmarshal([]byte(componentJSON(doc.Components)), &components) != nil {
		return ErrComponentInvalid
	}
	coverage, e := sourceidentity.CheckComponents(ctx, path, components)
	if e != nil || componentJSON(coverage) != componentJSON(expected) {
		return ErrComponentInvalid
	}
	return nil
}
func (s *Store) FreezeComponentPlan(ctx context.Context, id, revision, task, actor int64, resultHash string) (contracts.ComponentPlanVersion, error) {
	var empty contracts.ComponentPlanVersion
	result, status, e := s.ComponentOperationResult(ctx, task)
	if e != nil {
		return empty, e
	}
	if status != "completed" || result == nil || result.Error != nil || result.Operation.Kind != "normalize" || result.Operation.PlanID != contracts.TaskID(fmt.Sprint(id)) || result.Operation.Revision != revision || !result.Coverage.Valid || componentDigest(result) != resultHash {
		return empty, ErrComponentInvalid
	}
	plan, e := s.GetComponentPlan(ctx, id)
	if e != nil {
		return empty, e
	}
	if plan.Revision != revision || !sameJSON(plan.Source, result.Operation.Source) || !sameJSON(normalizedComponentDocument(plan.Document), result.Document) {
		return empty, ErrComponentConflict
	}
	snap, e := s.GetSnapshot(ctx, mustComponentID(plan.Source.SnapshotID))
	if e != nil {
		return empty, e
	}
	if e = verifyComponentEvidence(ctx, snap.SnapshotPath, string(plan.Source.ContentHash), *result.Document, *result.Coverage); e != nil {
		return empty, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	var current int64
	query := `SELECT revision FROM component_plans WHERE id=?`
	if s.db.postgres {
		query += ` FOR UPDATE`
	}
	if e = tx.QueryRowContext(ctx, query, id).Scan(&current); e != nil {
		return empty, e
	}
	if current != revision {
		return empty, ErrComponentConflict
	}
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT document_json FROM component_plan_versions WHERE plan_id=? AND revision=?`, id, revision).Scan(&existing)
	if e == nil {
		v, e := decodeComponentVersion(existing)
		return v, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return empty, e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM component_operations o JOIN scan_tasks t ON t.id=o.task_id WHERE o.plan_id=? AND o.revision=? AND t.status IN ('pending','claimed','scanning','cancel_requested')`, id, revision).Scan(&active); e != nil {
		return empty, e
	}
	if active > 0 {
		return empty, ErrComponentConflict
	}
	var version int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM component_plan_versions WHERE plan_id=?`, id).Scan(&version); e != nil {
		return empty, e
	}
	v := contracts.ComponentPlanVersion{SchemaVersion: 1, PlanID: plan.ID, Source: plan.Source, Version: version, Revision: revision, Document: *result.Document, Coverage: *result.Coverage, Origin: contracts.ComponentPlanVersionOrigin(plan.Origin), ValidationExecution: result.Execution, ResultSHA256: contracts.Digest(resultHash), ConfirmedBy: contracts.TaskID(fmt.Sprint(actor)), ConfirmedAt: contracts.Timestamp(utcComponentTime())}
	v.SHA256 = contracts.Digest(componentDigest(v))
	raw := componentJSON(v)
	if _, e = contracts.DecodeComponentPlanVersion([]byte(raw), nil); e != nil {
		return empty, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO component_plan_versions(plan_id,version,revision,document_json) VALUES(?,?,?,?)`, id, version, revision, raw); e != nil {
		return empty, e
	}
	return v, tx.Commit()
}
func mustComponentID(id contracts.TaskID) int64 {
	v, _ := strconv.ParseInt(string(id), 10, 64)
	return v
}
func decodeComponentVersion(raw string) (contracts.ComponentPlanVersion, error) {
	v, e := contracts.DecodeComponentPlanVersion([]byte(raw), nil)
	if e != nil {
		return v, e
	}
	hash := v.SHA256
	v.SHA256 = ""
	actual := componentDigest(v)
	v.SHA256 = hash
	if string(hash) != actual {
		return v, ErrComponentInvalid
	}
	return v, nil
}
func (s *Store) ComponentVersion(ctx context.Context, id, version int64) (contracts.ComponentPlanVersion, error) {
	var raw string
	var revision int64
	e := s.db.QueryRowContext(ctx, `SELECT revision,document_json FROM component_plan_versions WHERE plan_id=? AND version=?`, id, version).Scan(&revision, &raw)
	if e != nil {
		return contracts.ComponentPlanVersion{}, e
	}
	v, e := decodeComponentVersion(raw)
	if e == nil && (v.PlanID != contracts.TaskID(fmt.Sprint(id)) || v.Version != version || v.Revision != revision) {
		return v, ErrComponentInvalid
	}
	return v, e
}
func (s *Store) ComponentVersions(ctx context.Context, id int64, page int) (ComponentVersionPage, error) {
	out := ComponentVersionPage{Items: []ComponentVersionItem{}, PageSize: 50}
	if e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM component_plan_versions WHERE plan_id=?`, id).Scan(&out.Total); e != nil {
		return out, e
	}
	out.Page = pageNumber(page, out.Total, 50)
	rows, e := s.db.QueryContext(ctx, `SELECT version,revision,document_json FROM component_plan_versions WHERE plan_id=? ORDER BY version DESC LIMIT 50 OFFSET ?`, id, (out.Page-1)*50)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var version, revision int64
		if e = rows.Scan(&version, &revision, &raw); e != nil {
			return out, e
		}
		v, e := decodeComponentVersion(raw)
		if e != nil {
			return out, e
		}
		if v.PlanID != contracts.TaskID(fmt.Sprint(id)) || v.Version != version || v.Revision != revision {
			return out, ErrComponentInvalid
		}
		out.Items = append(out.Items, ComponentVersionItem{v.Version, v.Revision, string(v.SHA256), string(v.ConfirmedAt), string(v.ConfirmedBy)})
	}
	return out, rows.Err()
}

func ComponentResultDigest(v *contracts.ComponentPlanningResult) string { return componentDigest(v) }
