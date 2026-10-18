package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

var ErrInvalidProgress = errors.New("progress does not match the negotiated observation contract")

type ProgressHeartbeatResult struct {
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
	Accepted bool   `json:"accepted"`
}

// Admission, renewal and progress replacement share a row lock. Replayed or older
// snapshots renew a valid lease without replacing a newer projection.
func (s *Store) HeartbeatContractExecution(ctx context.Context, h contracts.ExecutionHeartbeat, lease time.Duration) (ProgressHeartbeatResult, error) {
	out := ProgressHeartbeatResult{}
	raw, e := contracts.EncodeJSON(h)
	if e != nil {
		return out, e
	}
	if _, e = contracts.DecodeExecutionHeartbeat(raw, nil); e != nil {
		return out, ErrInvalidProgress
	}
	id, e := strconv.ParseInt(string(h.Execution.TaskID), 10, 64)
	if e != nil {
		return out, ErrInvalidProgress
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var owner, status, expires, old string
	var attempt, version int
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(agent_id,''),attempt,status,COALESCE(lease_expires_at,''),observation_contract_version,progress_json FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&owner, &attempt, &status, &expires, &version, &old)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrLeaseLost
	}
	if e != nil {
		return out, e
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)
	if owner != string(h.AgentID) || attempt != int(h.Execution.Attempt) || !IsRunningStatus(status) || status == "pending" || expires < stamp {
		return out, ErrLeaseLost
	}
	if version != 1 {
		return out, ErrInvalidProgress
	}
	if old != "" {
		if p, err := contracts.DecodeProgressSnapshotV2([]byte(old), &h.Execution); err == nil {
			out.Revision = p.Revision
		}
	}
	stage := ""
	progress := ""
	if h.Progress != nil && h.Progress.Revision > out.Revision {
		bytes, err := contracts.EncodeJSON(h.Progress)
		if err != nil {
			return out, ErrInvalidProgress
		}
		progress = string(bytes)
		stage = string(h.Progress.Phase)
		out.Revision = h.Progress.Revision
		out.Accepted = true
	}
	diagnostic := ""
	if h.Diagnostic.Present {
		bytes, err := contracts.EncodeJSON(h.Diagnostic.Value)
		if err != nil {
			return out, err
		}
		diagnostic = string(bytes)
	}
	until := now.Add(lease).Format(time.RFC3339)
	if status == "claimed" {
		status = "scanning"
	}
	out.Status = status
	_, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET heartbeat_at=?,lease_expires_at=?,status=?,current_stage=CASE WHEN ?<>'' THEN ? ELSE current_stage END,progress_json=CASE WHEN ?<>'' THEN ? ELSE progress_json END,progress_diagnostic_json=? WHERE id=?`, stamp, until, status, stage, stage, progress, progress, diagnostic, id)
	if e != nil {
		return out, e
	}
	attemptStatus := status
	if attemptStatus == "cancel_requested" {
		attemptStatus = "scanning"
	}
	_, e = tx.ExecContext(ctx, `UPDATE scan_attempts SET status=?,lease_expires_at=?,progress_json=CASE WHEN ?<>'' THEN ? ELSE progress_json END,progress_diagnostic_json=? WHERE task_id=? AND attempt=? AND agent_id=?`, attemptStatus, until, progress, progress, diagnostic, id, h.Execution.Attempt, h.AgentID)
	if e != nil {
		return out, e
	}
	if out.Accepted {
		state, e := componentProgress(h.Progress)
		if e != nil {
			return out, ErrInvalidProgress
		}
		if e = s.recordComponentSnapshot(ctx, tx, id, h.Execution.Attempt, state); e != nil {
			return out, e
		}
		if e = s.recordProgressCost(ctx, tx, id, h.Execution.Attempt, h.Progress); e != nil {
			return out, e
		}
	}
	return out, tx.Commit()
}
