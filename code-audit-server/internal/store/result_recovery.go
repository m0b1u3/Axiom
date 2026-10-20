package store

import (
	"axiom/contracts"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrRecoveryConflict = errors.New("执行状态或恢复证据已变化，请刷新后重新确认")
var ErrRecoveryUnavailable = errors.New("本次执行没有可安全恢复的完整提交结果，已保留现有记录与证据")
var errRecoveryReplayed = errors.New("result recovery already committed")

const checkpointResumeReason = "当前 Web 的公开 SDK 集成不支持跨进程断点续扫。完整提交结果可恢复入库；重新执行会创建新的扫描会话。"

type resultRecovery struct {
	record             contracts.RecoveredResult
	expectedStatus     string
	outputPath         string
	previousCompletion *string
	previousError      string
}

func (s *Store) ResultRecovery(ctx context.Context, id int64, attempt int) (*contracts.RecoveredResult, error) {
	if !s.db.postgres {
		return nil, nil
	}
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM scan_result_recoveries WHERE task_id=? AND attempt=?`, id, attempt).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var v contracts.RecoveredResult
	if e = json.Unmarshal([]byte(raw), &v); e != nil {
		return nil, e
	}
	return &v, nil
}

// RecoverySnapshot is an observation, not permission to mutate. Every action
// repeats the identity, lease, cleanup and artifact checks under a task lock.
func (s *Store) RecoverySnapshot(ctx context.Context, id int64, outputRoot string) (contracts.ScanRecoverySnapshot, error) {
	t, e := s.Get(ctx, id)
	if e != nil {
		return contracts.ScanRecoverySnapshot{}, e
	}
	if s.db.postgres {
		if e = s.db.QueryRowContext(ctx, `SELECT COALESCE(lease_expires_at,'') FROM scan_tasks WHERE id=?`, id).Scan(&t.LeaseExpiresAt); e != nil {
			return contracts.ScanRecoverySnapshot{}, e
		}
	}
	v := contracts.ScanRecoverySnapshot{
		SchemaVersion: 1, Execution: contracts.RecoveryExecution{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(t.Attempt)},
		ObservedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano)), TaskStatus: contracts.ScanRecoverySnapshotTaskStatus(t.Status), SDKVersion: contracts.CurrentSDKVersion,
		CheckpointResume: contracts.ScanRecoverySnapshotCheckpointResume{Supported: false, Reason: checkpointResumeReason},
		CommittedResult:  contracts.ScanRecoverySnapshotCommittedResult{State: "unsupported", Reason: "历史执行缺少完整结果合同；原有报告与历史读取保持可用。"},
		Restart:          contracts.ScanRecoverySnapshotRestart{Allowed: IsTerminalStatus(t.Status), Reason: "重新执行沿用冻结的源码与配置，可能重复分析并再次产生费用；原执行记录保留。"},
		Interruption:     contracts.ScanRecoverySnapshotInterruption{Allowed: !IsTerminalStatus(t.Status) && recoveryLeaseEnded(t.Status, t.LeaseExpiresAt), Reason: "仅在执行租约已过期时登记中断，不启动模型；已取消的任务保持取消，未知费用仍需核对。"},
	}
	if t.InputContractVersion == 0 {
		v.Restart.Reason = "重新执行沿用历史任务的源码路径与配置规则；会创建新的会话，可能重复分析与费用，已有历史记录仍保留。"
	}
	if t.SDKOperation != nil || t.ComponentPlanning != nil {
		v.Restart.Allowed = false
		v.Interruption.Allowed = false
		v.Restart.Reason = "独立 SDK 操作需要在 Web 重新发起；漏洞复核必须再次确认未知费用。"
		v.CommittedResult.Reason = "此恢复入口仅用于扫描任务，独立 SDK 操作保留自己的结果与费用语义。"
		return v, nil
	}
	if !IsTerminalStatus(t.Status) {
		v.Restart.Reason = "当前任务尚未结束；浏览器断开不影响后台扫描，请先检查执行状态。"
	}
	budget, e := s.BudgetSnapshot(ctx, id)
	if e != nil {
		return v, e
	}
	if v.Restart.Allowed && !budget.RetryAllowed {
		v.Restart.Allowed = false
		v.Restart.Reason = "累计预算不足或存在未知费用；请先在 Web 核对费用并检查剩余额度。"
	}
	if t.ComponentScan != nil {
		v.Restart.Allowed = false
		v.Restart.Reason = "请在组件执行中选择失败或未完成组件重新执行；已完成组件与原始证据保留。"
	}
	if !s.db.postgres || t.ResultContractVersion != 1 || t.Attempt < 1 {
		return v, nil
	}
	v.Recovery, e = s.ResultRecovery(ctx, id, t.Attempt)
	if e != nil {
		return v, e
	}
	a, e := s.GetAttempt(ctx, id, t.Attempt)
	if e != nil {
		return v, e
	}
	archive, e := s.AttemptResult(ctx, id, t.Attempt)
	if e != nil {
		return v, e
	}
	if archive.Result != nil {
		if archive.Result.Engine.Version != nil && (*archive.Result.Engine.Version == "0.1.25" || *archive.Result.Engine.Version == "0.5.0") {
			v.SDKVersion = contracts.ScanRecoverySnapshotSDKVersion(*archive.Result.Engine.Version)
		}
		v.CommittedResult.State = "archived"
		v.CommittedResult.Reason = "完整结果已入库，可查看原执行；无需重新调用模型。"
		if v.Recovery != nil {
			h := v.Recovery.CommitSHA256
			v.CommittedResult.SHA256 = &h
		}
		return v, nil
	}
	if !recoveryLeaseEnded(t.Status, t.LeaseExpiresAt) || t.Status == "pending" {
		v.CommittedResult.State = "blocked"
		v.CommittedResult.Reason = "当前执行仍持有有效租约或任务已重新排队；不读取或接管正在写入的结果。"
		return v, nil
	}
	result, marker, e := readRecoveryCommit(ctx, outputRoot, a.OutputPath, contracts.ExecutionRef{TaskID: v.Execution.TaskID, Attempt: v.Execution.Attempt}, false)
	if e != nil {
		v.CommittedResult.State = "invalid"
		v.CommittedResult.Reason = "提交标记、结果校验或执行身份不完整，不能恢复入库；已保留现有证据。"
		if errors.Is(e, os.ErrNotExist) {
			v.CommittedResult.State = "missing"
			v.CommittedResult.Reason = "没有完整提交标记；部分日志仍可查看，无法据此续扫或推定结果完整。"
		}
		return v, nil
	}
	model, e := contracts.DecodeResolvedModel([]byte(t.ModelSnapshotJSON), nil)
	if e != nil || !sameJSON(model, result.Model) {
		v.CommittedResult.State = "invalid"
		v.CommittedResult.Reason = "结果模型与本次执行的冻结配置不一致，不能恢复入库。"
		return v, nil
	}
	v.CommittedResult.State = "recoverable"
	v.CommittedResult.SHA256 = &marker.SHA256
	v.CommittedResult.Reason = "检测到本次执行的完整提交结果；可校验原始产物并恢复入库，不调用模型，也不创建新执行。"
	return v, nil
}

func recoveryLeaseEnded(status, lease string) bool {
	if IsTerminalStatus(status) {
		return true
	}
	if status != "claimed" && status != "scanning" && status != "cancel_requested" {
		return false
	}
	if lease == "" {
		return true
	} // No valid lease exists; normal completion also rejects it.
	until, e := time.Parse(time.RFC3339Nano, lease)
	return e == nil && !until.After(time.Now().UTC())
}

// This explicit Web action makes an expired execution terminal even when no
// Agent is online to poll the queue. The old lease can never submit afterward.
func (s *Store) MarkExecutionInterrupted(ctx context.Context, id int64, action contracts.ScanRecoveryAction) error {
	if !s.db.postgres || action.Action != "interrupt_expired" || action.Execution.TaskID != contracts.TaskID(fmt.Sprint(id)) {
		return ErrRecoveryUnavailable
	}
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var status, lease string
	var attempt int
	if e = tx.QueryRowContext(ctx, `SELECT status,attempt,COALESCE(lease_expires_at,'') FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&status, &attempt, &lease); e != nil {
		return e
	}
	if attempt != int(action.Execution.Attempt) || status != string(action.ExpectedStatus) || IsTerminalStatus(status) || !recoveryLeaseEnded(status, lease) {
		return ErrRecoveryConflict
	}
	var operations int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sdk_operations WHERE task_id=?)+(SELECT COUNT(*) FROM component_operations WHERE task_id=?)`, id, id).Scan(&operations); e != nil {
		return e
	}
	if operations > 0 {
		return ErrRecoveryUnavailable
	}
	if e = s.cleanupBlocksTask(ctx, tx, id); e != nil {
		return e
	}
	terminal, code, message := "failed", "lease_expired", "执行租约已过期，由 Web 登记中断；未重新执行，最终费用未知"
	if status == "cancel_requested" {
		terminal, code, message = "canceled", "canceled", "取消请求的执行已失联；由 Web 确认取消，未重新执行，最终费用未知"
	}
	now := securityTime(time.Now())
	if _, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET status=?,error_code=?,error_message=?,finished_at=?,lease_expires_at=NULL WHERE id=?`, terminal, code, message, now, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE scan_attempts SET status=?,error_code=?,error_message=?,finished_at=?,lease_expires_at=NULL WHERE task_id=? AND attempt=?`, terminal, code, message, now, id, attempt); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET state='unknown',reason='execution_interrupted',updated_at=? WHERE task_id=? AND attempt=? AND state='active'`, now, id, attempt); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, id); e != nil {
		return e
	}
	return tx.Commit()
}

// RestoreCommittedResult does not run Scanner or SDK. The existing atomic
// marker is the only source of a result; raw JSON and session logs alone never
// qualify. No caller-supplied filesystem path is accepted.
func (s *Store) RestoreCommittedResult(ctx context.Context, id int64, action contracts.ScanRecoveryAction, outputRoot, actor string) error {
	if !s.db.postgres || action.Action != "restore_result" || action.ResultSHA256 == nil || action.Execution.TaskID != contracts.TaskID(fmt.Sprint(id)) || action.Execution.Attempt < 1 || actor == "" {
		return ErrRecoveryUnavailable
	}
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()
	t, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COALESCE(lease_expires_at,'') FROM scan_tasks WHERE id=?`, id).Scan(&t.LeaseExpiresAt); e != nil {
		return e
	}
	if t.Attempt != int(action.Execution.Attempt) || (t.SDKOperation != nil || t.ComponentPlanning != nil) || t.Status == "pending" {
		return ErrRecoveryConflict
	}
	previous, e := s.ResultRecovery(ctx, id, t.Attempt)
	if e != nil {
		return e
	}
	if previous != nil {
		if previous.CommitSHA256 == *action.ResultSHA256 && string(previous.PreviousStatus) == string(action.ExpectedStatus) {
			return nil
		}
		return ErrRecoveryConflict
	}
	if t.Status != string(action.ExpectedStatus) || !recoveryLeaseEnded(t.Status, t.LeaseExpiresAt) {
		return ErrRecoveryConflict
	}
	a, e := s.GetAttempt(ctx, id, t.Attempt)
	if e != nil {
		return e
	}
	ref := contracts.ExecutionRef{TaskID: action.Execution.TaskID, Attempt: action.Execution.Attempt}
	result, marker, e := readRecoveryCommit(ctx, outputRoot, a.OutputPath, ref, true)
	if e != nil {
		return fmt.Errorf("%w: 提交结果或原始产物校验未通过", ErrRecoveryUnavailable)
	}
	if marker.SHA256 != *action.ResultSHA256 {
		return ErrRecoveryConflict
	}
	full := contracts.ExecutionCompletion{SchemaVersion: 1, Execution: ref, AgentID: contracts.Identifier(a.AgentID), ExecutionStatus: result.Status, Process: contracts.ProcessExit{ResultCommitted: true}, Result: contracts.Some(*result)}
	// Recover evidence without turning a user cancel or timeout into success.
	if t.Status == "canceled" || t.Status == "cancel_requested" || t.Status == "timeout" {
		status := t.Status
		if status == "cancel_requested" {
			status = "canceled"
		}
		full.ExecutionStatus = contracts.TerminalStatus(status)
		full.Error = contracts.Some(contracts.ContractError{Code: contracts.ErrorCode(status), Origin: contracts.ErrorOriginServer, Message: "原执行已停止；仅恢复已提交结果，未重新执行扫描", Retryable: false})
	}
	record := contracts.RecoveredResult{CommitSHA256: marker.SHA256, RecoveredAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano)), RecoveredBy: actor, PreviousStatus: contracts.RecoveredResultPreviousStatus(t.Status), PreviousErrorCode: t.ErrorCode, PreviousErrorMessage: t.ErrorMessage}
	return s.completeContractExecution(ctx, full, &resultRecovery{record: record, expectedStatus: t.Status, outputPath: a.OutputPath, previousError: t.ErrorMessage})
}

func (s *Store) authorizeResultRecovery(ctx context.Context, tx *transaction, c Completion, status, lease string) error {
	var priorRaw string
	e := tx.QueryRowContext(ctx, `SELECT document_json FROM scan_result_recoveries WHERE task_id=? AND attempt=?`, c.TaskID, c.Attempt).Scan(&priorRaw)
	if e == nil {
		var prior contracts.RecoveredResult
		if json.Unmarshal([]byte(priorRaw), &prior) == nil && prior.CommitSHA256 == c.recovery.record.CommitSHA256 && string(prior.PreviousStatus) == c.recovery.expectedStatus {
			return errRecoveryReplayed
		}
		return ErrRecoveryConflict
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if c.contract == nil || !c.contract.Result.Present || status != c.recovery.expectedStatus || !recoveryLeaseEnded(status, lease) {
		return ErrRecoveryConflict
	}
	if e := s.cleanupBlocksTask(ctx, tx, c.TaskID); e != nil {
		return e
	}
	var output string
	var raw sql.NullString
	if e := tx.QueryRowContext(ctx, `SELECT output_path,result_json FROM scan_attempts WHERE task_id=? AND attempt=? AND agent_id=?`, c.TaskID, c.Attempt, c.AgentID).Scan(&output, &raw); e != nil {
		return e
	}
	if output != c.recovery.outputPath {
		return ErrRecoveryConflict
	}
	priorBudget, e := s.readBudget(ctx, tx, c.TaskID)
	if e != nil {
		return e
	}
	for _, cost := range priorBudget.snapshot.Attempts {
		if cost.Execution.Attempt == int64(c.Attempt) {
			c.recovery.record.PreviousCost = &cost
			break
		}
	}
	if raw.Valid {
		old, e := contracts.DecodeExecutionCompletion([]byte(raw.String), &c.contract.Execution)
		if e != nil || old.Result.Present {
			return ErrRecoveryConflict
		}
		c.recovery.previousCompletion = &raw.String
		c.recovery.record.PreviousCompletion = &old
	}
	return nil
}

func saveResultRecovery(ctx context.Context, tx *transaction, c Completion) error {
	raw, e := contracts.EncodeJSON(c.recovery.record)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO scan_result_recoveries(task_id,attempt,commit_sha256,document_json,previous_completion_json,previous_error_message) VALUES(?,?,?,?,?,?)`, c.TaskID, c.Attempt, c.recovery.record.CommitSHA256, string(raw), c.recovery.previousCompletion, c.recovery.previousError)
	return e
}

// os.Root confines every read to the configured shared output root. Reject
// symlinks as well, including replaced parent directories. Hash the exact
// private byte snapshot that is subsequently decoded and archived.
func readRecoveryCommit(ctx context.Context, base, output string, ref contracts.ExecutionRef, verifyArtifacts bool) (*contracts.ScanResult, *contracts.ResultCommit, error) {
	expected := filepath.Join(base, string(ref.TaskID), fmt.Sprintf("attempt-%d", ref.Attempt))
	if base == "" || filepath.Clean(output) != filepath.Clean(expected) {
		return nil, nil, ErrRecoveryUnavailable
	}
	root, e := os.OpenRoot(base)
	if e != nil {
		return nil, nil, e
	}
	defer root.Close()
	prefix := filepath.Join(string(ref.TaskID), fmt.Sprintf("attempt-%d", ref.Attempt))
	read := func(name string, limit int) ([]byte, error) {
		f, e := openRecoveryArtifact(root, filepath.Join(prefix, filepath.FromSlash(name)))
		if e != nil {
			return nil, e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Size() > int64(limit) {
			return nil, ErrRecoveryUnavailable
		}
		b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
		if e != nil {
			return nil, e
		}
		if len(b) > limit {
			return nil, ErrRecoveryUnavailable
		}
		return b, nil
	}
	raw, e := read("normalized/result.commit.json", contracts.MaxProgressBytes)
	if e != nil {
		return nil, nil, e
	}
	marker, e := contracts.DecodeResultCommit(raw, &ref)
	if e != nil || marker.RelativePath != "normalized/result.json" {
		return nil, nil, ErrRecoveryUnavailable
	}
	data, e := read(string(marker.RelativePath), contracts.MaxDocumentBytes)
	if e != nil {
		return nil, nil, e
	}
	h := sha256.Sum256(data)
	if int64(len(data)) != marker.SizeBytes || hex.EncodeToString(h[:]) != string(marker.SHA256) {
		return nil, nil, ErrRecoveryUnavailable
	}
	result, e := contracts.DecodeScanResult(data, &ref)
	if e != nil {
		return nil, nil, ErrRecoveryUnavailable
	}
	if verifyArtifacts {
		var total int64
		for _, a := range result.Artifacts {
			if e = ctx.Err(); e != nil {
				return nil, nil, e
			}
			total += int64(a.SizeBytes)
			if total > 512<<20 {
				return nil, nil, ErrRecoveryUnavailable
			}
			f, e := openRecoveryArtifact(root, filepath.Join(prefix, filepath.FromSlash(string(a.RelativePath))))
			if e != nil {
				return nil, nil, e
			}
			hash := sha256.New()
			n, e := io.Copy(hash, io.LimitReader(f, int64(a.SizeBytes)+1))
			f.Close()
			if e != nil || n != int64(a.SizeBytes) || hex.EncodeToString(hash.Sum(nil)) != string(a.SHA256) {
				return nil, nil, ErrRecoveryUnavailable
			}
		}
	}
	return &result, &marker, nil
}

func openRecoveryArtifact(root *os.Root, name string) (*os.File, error) {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := range parts {
		info, e := root.Lstat(filepath.Join(parts[:i+1]...))
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return nil, ErrRecoveryUnavailable
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, ErrRecoveryUnavailable
		}
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrRecoveryUnavailable
	}
	return f, nil
}
