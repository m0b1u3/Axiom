package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type InterimReviewStart struct {
	Envelope          contracts.InterimReviewEnvelope
	RequestID         string
	Retry             bool
	GroupID, SourceID int64
}

func (s *Store) prepareInterimReview(ctx context.Context, tx *transaction, t *Task) (int64, error) {
	start := t.InterimReviewStart
	if !s.IsPostgres() || t.SDKOperation == nil || t.SDKOperation.Kind != "validate" || t.SDKOperation.Origin == nil {
		return 0, errors.New("候选复核需要冻结的独立 SDK 操作")
	}
	input, e := contracts.ReadInterimReview(start.Envelope, t.SourceContentHash)
	if e != nil {
		return 0, e
	}
	var prior int64
	var sha string
	e = tx.QueryRowContext(ctx, `SELECT task_id,input_sha256 FROM interim_review_requests WHERE request_id=?`, start.RequestID).Scan(&prior, &sha)
	if e == nil {
		if sha != string(start.Envelope.InputSHA256) {
			return 0, errors.New("复核请求编号已用于其他证据")
		}
		return prior, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	key := fmt.Sprint(t.ProjectID, ":", input.SourceContentHash, ":", input.GroupKey)
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(?,740922))`, key); e != nil {
		return 0, e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, e = tx.ExecContext(ctx, `INSERT INTO interim_review_groups(source_hash,group_key,project_id,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, input.SourceContentHash, input.GroupKey, t.ProjectID, now); e != nil {
		return 0, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT id FROM interim_review_groups WHERE source_hash=? AND group_key=? AND project_id=? FOR UPDATE`, input.SourceContentHash, input.GroupKey, t.ProjectID).Scan(&start.GroupID); e != nil {
		return 0, e
	}
	// Check idempotency again after the grouping lock (concurrent submissions).
	e = tx.QueryRowContext(ctx, `SELECT task_id,input_sha256 FROM interim_review_requests WHERE request_id=?`, start.RequestID).Scan(&prior, &sha)
	if e == nil {
		if sha != string(start.Envelope.InputSHA256) {
			return 0, errors.New("复核请求编号冲突")
		}
		return prior, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	origin := t.SDKOperation.Origin
	if _, e = tx.ExecContext(ctx, `INSERT INTO interim_review_sources(group_id,origin_task_id,origin_attempt,input_json,input_sha256,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, start.GroupID, origin.TaskID, origin.Attempt, start.Envelope.InputJSON, start.Envelope.InputSHA256, now); e != nil {
		return 0, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT id FROM interim_review_sources WHERE group_id=? AND input_sha256=?`, start.GroupID, start.Envelope.InputSHA256).Scan(&start.SourceID); e != nil {
		return 0, e
	}
	var status string
	e = tx.QueryRowContext(ctx, `SELECT r.task_id,t.status FROM interim_reviews r JOIN scan_tasks t ON t.id=r.task_id WHERE r.group_id=? ORDER BY r.task_id DESC LIMIT 1`, start.GroupID).Scan(&prior, &status)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	if e == nil {
		var archived bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM interim_review_archives WHERE group_id=?)`, start.GroupID).Scan(&archived); e != nil {
			return 0, e
		}
		if !start.Retry || !IsTerminalStatus(status) || archived {
			_, e = tx.ExecContext(ctx, `INSERT INTO interim_review_requests(request_id,input_sha256,group_id,task_id) VALUES(?,?,?,?)`, start.RequestID, start.Envelope.InputSHA256, start.GroupID, prior)
			return prior, e
		}
	}
	id := strconv.FormatInt(start.GroupID, 10)
	t.SDKOperation.FindingID = &id
	raw, e := contracts.EncodeJSON(start.Envelope)
	if e != nil {
		return 0, e
	}
	finding := contracts.JSONObject{"axiomInterimReview": raw}
	t.SDKOperation.Finding = &finding
	return 0, nil
}
func (s *Store) saveInterimReview(ctx context.Context, tx *transaction, t Task, id int64) error {
	if t.InterimReviewStart == nil {
		return nil
	}
	v := t.InterimReviewStart
	if _, e := tx.ExecContext(ctx, `INSERT INTO interim_reviews(task_id,group_id,source_id,created_at) VALUES(?,?,?,?)`, id, v.GroupID, v.SourceID, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO interim_review_requests(request_id,input_sha256,group_id,task_id) VALUES(?,?,?,?)`, v.RequestID, v.Envelope.InputSHA256, v.GroupID, id)
	return e
}

type InterimReviewHistory struct {
	TaskID      int64                           `json:"taskId"`
	Attempt     int                             `json:"attempt"`
	Status      string                          `json:"status"`
	InputSHA256 string                          `json:"inputSha256"`
	Outcome     *contracts.InterimReviewOutcome `json:"outcome"`
	Report      string                          `json:"report"`
	Error       string                          `json:"error"`
}
type InterimReviewSource struct {
	Execution   contracts.ExecutionRef          `json:"execution"`
	InputSHA256 string                          `json:"inputSha256"`
	Members     []contracts.InterimReviewMember `json:"members"`
}
type InterimReviewView struct {
	GroupID     int64                               `json:"groupId"`
	Input       contracts.InterimReviewInput        `json:"input"`
	InputSHA256 string                              `json:"inputSha256"`
	History     []InterimReviewHistory              `json:"history"`
	Sources     []InterimReviewSource               `json:"sources"`
	Archive     *contracts.InterimReviewArchive     `json:"archive"`
	SourceCheck *contracts.InterimReviewSourceCheck `json:"sourceCheck"`
	FindingID   int64                               `json:"findingId"`
}

func (s *Store) InterimReviewGroups(ctx context.Context, task int64, attempt int) ([]int64, error) {
	out := []int64{}
	if !s.IsPostgres() {
		return out, nil
	}
	rows, e := s.db.QueryContext(ctx, `SELECT DISTINCT group_id FROM interim_review_sources WHERE origin_task_id=? AND origin_attempt=? ORDER BY group_id DESC`, task, attempt)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *Store) InterimReview(ctx context.Context, group int64) (*InterimReviewView, error) {
	if !s.IsPostgres() {
		return nil, sql.ErrNoRows
	}
	v := &InterimReviewView{GroupID: group, History: []InterimReviewHistory{}, Sources: []InterimReviewSource{}}
	rows, e := s.db.QueryContext(ctx, `SELECT origin_task_id,origin_attempt,input_json,input_sha256 FROM interim_review_sources WHERE group_id=? ORDER BY id DESC`, group)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		var attempt int
		var raw, sha string
		if e = rows.Scan(&id, &attempt, &raw, &sha); e != nil {
			rows.Close()
			return nil, e
		}
		input, err := contracts.DecodeInterimReviewInput([]byte(raw), nil)
		if err == nil {
			input, err = contracts.ReadInterimReview(contracts.InterimReviewEnvelope{SchemaVersion: 1, InputJSON: raw, InputSHA256: contracts.Digest(sha)}, string(input.SourceContentHash))
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		if v.InputSHA256 == "" {
			v.Input = input
			v.InputSHA256 = sha
		}
		v.Sources = append(v.Sources, InterimReviewSource{Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt)}, InputSHA256: sha, Members: input.Members})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(v.Sources) == 0 {
		return nil, sql.ErrNoRows
	}
	rows, e = s.db.QueryContext(ctx, `SELECT r.task_id,t.attempt,t.status,src.input_sha256,COALESCE(t.error_message,'') FROM interim_reviews r JOIN scan_tasks t ON t.id=r.task_id JOIN interim_review_sources src ON src.id=r.source_id WHERE r.group_id=? ORDER BY r.task_id DESC`, group)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var h InterimReviewHistory
		if e = rows.Scan(&h.TaskID, &h.Attempt, &h.Status, &h.InputSHA256, &h.Error); e != nil {
			rows.Close()
			return nil, e
		}
		v.History = append(v.History, h)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(v.History) > 0 {
		var raw, sha string
		e = s.db.QueryRowContext(ctx, `SELECT s.input_json,s.input_sha256 FROM interim_reviews r JOIN interim_review_sources s ON s.id=r.source_id WHERE r.task_id=?`, v.History[0].TaskID).Scan(&raw, &sha)
		if e != nil {
			return nil, e
		}
		input, err := contracts.DecodeInterimReviewInput([]byte(raw), nil)
		if err != nil {
			return nil, err
		}
		v.Input = input
		v.InputSHA256 = sha
	}
	for i := range v.History {
		h := &v.History[i]
		if h.Attempt < 1 {
			continue
		}
		var raw sql.NullString
		if e = s.db.QueryRowContext(ctx, `SELECT result_json FROM scan_attempts WHERE task_id=? AND attempt=?`, h.TaskID, h.Attempt).Scan(&raw); e != nil {
			return nil, e
		}
		if !raw.Valid {
			continue
		}
		ref := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(h.TaskID)), Attempt: int64(h.Attempt)}
		full, err := contracts.DecodeExecutionCompletion([]byte(raw.String), &ref)
		if err != nil {
			return nil, err
		}
		if !full.Result.Present {
			continue
		}
		op, err := OperationResult(full.Result.Value)
		if err != nil {
			return nil, err
		}
		if op != nil && op.Validation != nil {
			h.Report = op.Validation.Report
			if h.Error == "" && contracts.ValidationReportBodyState(h.Report) != "body_present" {
				h.Error = "复核报告输出不完整：报告结构不完整，未识别到独立评估正文（正文可能粘连在标题行）；原始 SDK 标签保留供诊断，候选仍待核查。"
			}
		}
		if b, ok := full.Result.Value.Extensions.Value["interimReviewOutcome"]; ok {
			out, err := contracts.DecodeInterimReviewOutcome(b, &ref)
			if err != nil {
				return nil, err
			}
			if string(out.InputSHA256) != h.InputSHA256 || out.SourceContentHash != v.Input.SourceContentHash || string(out.ReportSHA256) != contracts.InterimReviewDigest([]byte(h.Report)) {
				return nil, errors.New("复核结论或原报告哈希不匹配")
			}
			h.Outcome = &out
		}
	}
	var archive string
	e = s.db.QueryRowContext(ctx, `SELECT document_json,finding_id FROM interim_review_archives WHERE group_id=?`, group).Scan(&archive, &v.FindingID)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if archive != "" {
		a, err := contracts.DecodeInterimReviewArchive([]byte(archive), nil)
		if err != nil {
			return nil, err
		}
		v.Archive = &a
	}
	if len(v.History) > 0 && v.History[0].Outcome != nil {
		task, err := s.Get(ctx, v.History[0].TaskID)
		if err != nil {
			return nil, err
		}
		checked := CheckInterimReviewSources(ctx, task.SourcePath, task.SourceContentHash, v.History[0].Report)
		v.SourceCheck = &checked
	}
	return v, nil
}
func (s *Store) InterimReviewRetryInput(ctx context.Context, group int64) (contracts.InterimReviewEnvelope, int64, int, error) {
	var raw, sha string
	var task int64
	var attempt int
	e := s.db.QueryRowContext(ctx, `SELECT s.input_json,s.input_sha256,s.origin_task_id,s.origin_attempt FROM interim_reviews r JOIN interim_review_sources s ON s.id=r.source_id WHERE r.group_id=? ORDER BY r.task_id DESC LIMIT 1`, group).Scan(&raw, &sha, &task, &attempt)
	return contracts.InterimReviewEnvelope{SchemaVersion: 1, InputJSON: raw, InputSHA256: contracts.Digest(sha)}, task, attempt, e
}
func (s *Store) validateInterimReviewCompletion(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	if !s.IsPostgres() {
		return nil
	}
	var input, sha string
	e := s.db.QueryRowContext(ctx, `SELECT s.input_json,s.input_sha256 FROM interim_reviews r JOIN interim_review_sources s ON s.id=r.source_id WHERE r.task_id=?`, id).Scan(&input, &sha)
	if errors.Is(e, sql.ErrNoRows) {
		if full.Result.Present && full.Result.Value.Extensions.Present {
			if _, ok := full.Result.Value.Extensions.Value["interimReviewOutcome"]; ok {
				return ErrInvalidResult
			}
		}
		return nil
	}
	if e != nil {
		return e
	}
	if !full.Result.Present {
		return nil
	}
	result := full.Result.Value
	op, err := OperationResult(result)
	if err != nil || op == nil {
		return ErrInvalidResult
	}
	if op.Validation == nil {
		return nil
	}
	raw, ok := result.Extensions.Value["interimReviewOutcome"]
	if !ok {
		return ErrInvalidResult
	}
	out, err := contracts.DecodeInterimReviewOutcome(raw, &full.Execution)
	var in contracts.InterimReviewInput
	if json.Unmarshal([]byte(input), &in) != nil {
		return ErrInvalidResult
	}
	if err != nil || string(out.InputSHA256) != sha || out.SourceContentHash != in.SourceContentHash || string(out.Disposition) != string(op.Validation.Disposition) || string(out.ReportSHA256) != contracts.InterimReviewDigest([]byte(op.Validation.Report)) {
		return ErrInvalidResult
	}
	return nil
}
