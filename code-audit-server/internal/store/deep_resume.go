package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

//go:embed migrations/025_deep_resume.sql
var deepResumeSQL string
var ErrDeepResume = errors.New("Deep 续扫条件已变化或不满足，请重新核验；未自动重新扫描")

func deepToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func deepDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func DeepConfigurationHash(t *Task) string {
	raw, _ := json.Marshal(struct {
		Model, Policy, Source string
		Input                 *contracts.ScanInput
		Parameters            *contracts.SDKScanParameters
		Identity              *contracts.SDKIdentityRef
	}{t.ModelSnapshotJSON, t.PolicySnapshotJSON, t.SourceContentHash, t.ScanInput, t.SDKParameters, t.SDKIdentity})
	return deepDigest(string(raw))
}
func (s *Store) loadDeepResume(ctx context.Context, t *Task) error {
	if !s.db.postgres {
		return nil
	}
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM deep_resume_runs WHERE task_id=? AND attempt=? AND status<>'canceled'`, t.ID, t.Attempt).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	v, e := contracts.DecodeDeepResumeAssignment([]byte(raw), nil)
	if e != nil {
		return e
	}
	t.DeepResume = &v
	return nil
}
func (s *Store) deepAgentReady(ctx context.Context, agent string) bool {
	var raw string
	var seen *time.Time
	if s.db.QueryRowContext(ctx, `SELECT runtime_json,runtime_seen_at FROM execution_agents WHERE agent_id=?`, agent).Scan(&raw, &seen) != nil || seen == nil || time.Since(*seen) > 30*time.Second {
		return false
	}
	v, e := contracts.DecodeAgentRuntime([]byte(raw), nil)
	return e == nil && v.State == contracts.AgentRuntimeStateReady && v.MatrixHash == control.MatrixHash && v.ControlAPI == 4 && v.ScannerDescriptionVersion != nil && *v.ScannerDescriptionVersion == 14
}
func (s *Store) deepIdentityReady(ctx context.Context, t *Task) bool {
	if t.SDKIdentity == nil {
		return true
	}
	var valid bool
	e := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sdk_identities WHERE id=? AND generation=? AND agent_id=? AND kind=? AND state='authenticated' AND authenticated=TRUE AND cleanup_pending=FALSE)`, t.SDKIdentity.ID, t.SDKIdentity.Generation, t.SDKIdentity.AgentID, t.SDKIdentity.Kind).Scan(&valid)
	return e == nil && valid
}
func deepCandidate(t *Task) bool {
	return t.ScanMode == "deep" && t.Attempt > 0 && t.InputContractVersion > 0 && t.ResultContractVersion == 1 && t.ObservationContractVersion == 1 && t.ComponentScan == nil && t.ComponentPlanning == nil && t.SDKOperation == nil && t.GitTarget == nil && (t.Status == "failed" || t.Status == "timeout")
}
func (s *Store) DeepResumeSnapshot(ctx context.Context, id int64) (contracts.DeepResumeSnapshot, error) {
	t, e := s.Get(ctx, id)
	if e != nil {
		return contracts.DeepResumeSnapshot{}, e
	}
	v := contracts.DeepResumeSnapshot{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(t.Attempt)}, State: "unsupported", Reason: "断点续扫仅适用于有原生恢复状态的 SDK 0.5.0 Deep 扫描；其他历史任务仍可读取。", Continuation: t.DeepResume}
	if !s.db.postgres || t.Attempt < 1 {
		return v, nil
	}
	if t.Status == "pending" {
		var pending string
		e := s.db.QueryRowContext(ctx, `SELECT document_json FROM deep_resume_runs WHERE task_id=? AND attempt=? AND status='pending'`, id, t.Attempt+1).Scan(&pending)
		if e == nil {
			next, err := contracts.DecodeDeepResumeAssignment([]byte(pending), nil)
			if err != nil {
				return v, err
			}
			v.Continuation = &next
			v.State = "pending"
			v.Reason = "续扫已确认，等待原 Agent 领取；请勿重复启动。"
			return v, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return v, e
		}
	}
	if t.DeepResume != nil {
		v.State = "used"
		v.Reason = "本次执行复用原 SDK Scan ID 与会话，费用按新增估算记录；原执行保留。"
		if !IsTerminalStatus(t.Status) {
			v.State = "running"
		}
		if !deepCandidate(t) {
			return v, nil
		}
	}
	if !deepCandidate(t) {
		if !IsTerminalStatus(t.Status) {
			v.State = "blocked"
			v.Reason = "当前执行仍在运行或排队，请继续观察；关闭浏览器不影响扫描。"
		}
		return v, nil
	}
	v.State = "idle"
	v.CanCheck = s.deepAgentReady(ctx, t.AgentID) && s.deepIdentityReady(ctx, t)
	v.Reason = "先由原 Agent 无模型核验 SDK 状态、原会话、源码和检查点。"
	if !v.CanCheck {
		v.Reason = "原 Agent 离线、未就绪或不支持本版续扫；不能换节点或复制会话启动。"
	}
	var probe, state, raw, updated, leaseUntil string
	e = s.db.QueryRowContext(ctx, `SELECT id,state,inspection_json,updated_at,lease_until FROM deep_resume_checks WHERE task_id=? AND attempt=? ORDER BY created_at DESC,id DESC LIMIT 1`, id, t.Attempt).Scan(&probe, &state, &raw, &updated, &leaseUntil)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	v.ProbeID = &probe
	switch state {
	case "queued", "checking":
		v.State = contracts.DeepResumeSnapshotState(state)
		v.Reason = "正在由原 Agent 检查恢复条件，不调用模型。"
		stamp, _ := time.Parse(time.RFC3339Nano, updated)
		until, _ := time.Parse(time.RFC3339Nano, leaseUntil)
		if (state == "checking" && time.Now().After(until)) || time.Since(stamp) > 2*time.Minute {
			v.State = "blocked"
			v.Reason = "恢复检查已超时；未调用模型，可在原 Agent 就绪后重新核验。"
		}
	case "ready":
		p, e := contracts.DecodeDeepResumeInspection([]byte(raw), &v.Execution)
		if e != nil {
			return v, e
		}
		v.Inspection = &p
		v.State = "blocked"
		v.Reason = p.Reason
		at, _ := time.Parse(time.RFC3339Nano, updated)
		if p.Eligible && time.Since(at) < 2*time.Minute && v.CanCheck {
			v.State = "ready"
			v.CanResume = true
		}
		if time.Since(at) >= 2*time.Minute {
			v.Reason = "恢复检查已过期，请重新核验。"
		}
		if p.ConfigurationHash != DeepConfigurationHash(t) || p.SourceHash != t.SourceContentHash {
			v.CanResume = false
			v.State = "blocked"
			v.Reason = "冻结配置或源码身份已改变，不能复用旧检查。"
		}
		budget, e := s.BudgetSnapshot(ctx, id)
		if e != nil {
			return v, e
		}
		if !budget.RetryAllowed {
			v.CanResume = false
			v.State = "blocked"
			v.Reason = "任务总预算不足或有未知费用，请先在 Web 对账。"
		}
		if (t.MaxCostUSD != nil || budget.TotalLimitMicros != nil) && p.Baseline.EstimatedUSD == nil {
			v.CanResume = false
			v.State = "blocked"
			v.Reason = "原生累计估算未知，不能安全计算本次续扫的预算上限。"
		}
	case "used":
		v.State = "pending"
		v.Reason = "续扫已确认，等待原 Agent 领取。"
	default:
		v.State = "blocked"
		v.Reason = "上次检查失效或执行条件已变化，请重新核验。"
	}
	return v, nil
}
func (s *Store) RequestDeepResumeCheck(ctx context.Context, id int64, observed contracts.ExecutionRef) (string, error) {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return "", e
	}
	defer release()
	t, e := s.Get(ctx, id)
	if e != nil {
		return "", e
	}
	if !deepCandidate(t) || observed.TaskID != contracts.TaskID(fmt.Sprint(id)) || observed.Attempt != int64(t.Attempt) || !s.deepAgentReady(ctx, t.AgentID) || !s.deepIdentityReady(ctx, t) {
		return "", ErrDeepResume
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var attempt int
	var status string
	if e = tx.QueryRowContext(ctx, `SELECT attempt,status FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&attempt, &status); e != nil {
		return "", e
	}
	if attempt != t.Attempt || status != t.Status {
		return "", ErrDeepResume
	}
	if e = s.cleanupBlocksTask(ctx, tx, id); e != nil {
		return "", e
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, e = tx.ExecContext(ctx, `UPDATE deep_resume_checks SET state='expired' WHERE task_id=? AND state IN ('queued','checking','ready')`, id); e != nil {
		return "", e
	}
	origin := attempt
	if t.DeepResume != nil {
		origin = int(t.DeepResume.Inspection.Origin.Attempt)
	}
	probe := deepToken()
	_, e = tx.ExecContext(ctx, `INSERT INTO deep_resume_checks(id,task_id,attempt,origin_attempt,agent_id,configuration_hash,source_hash,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'queued',?,?)`, probe, id, attempt, origin, t.AgentID, DeepConfigurationHash(t), t.SourceContentHash, stamp, stamp)
	if e != nil {
		return "", e
	}
	return probe, tx.Commit()
}
func (s *Store) ClaimDeepResumeCheck(ctx context.Context, agent string) (*contracts.DeepResumeProbeClaim, error) {
	if !s.deepAgentReady(ctx, agent) {
		return nil, nil
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, e = tx.ExecContext(ctx, `UPDATE deep_resume_checks SET state='expired' WHERE state='checking' AND lease_until<?`, stamp); e != nil {
		return nil, e
	}
	var id, config, source string
	var task, attempt, origin int64
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.task_id,c.attempt,c.origin_attempt,c.configuration_hash,c.source_hash FROM deep_resume_checks c JOIN scan_tasks t ON t.id=c.task_id WHERE c.state='queued' AND c.agent_id=? AND t.agent_id=? AND t.attempt=c.attempt AND t.status IN ('failed','timeout') ORDER BY c.created_at LIMIT 1 FOR UPDATE OF c SKIP LOCKED`, agent, agent).Scan(&id, &task, &attempt, &origin, &config, &source)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	t, e := s.Get(ctx, task)
	if e != nil {
		return nil, e
	}
	if !deepCandidate(t) || !s.deepIdentityReady(ctx, t) || DeepConfigurationHash(t) != config || source != t.SourceContentHash {
		return nil, ErrDeepResume
	}
	token := deepToken()
	if _, e = tx.ExecContext(ctx, `UPDATE deep_resume_checks SET state='checking',lease_hash=?,lease_until=?,updated_at=? WHERE id=?`, deepDigest(token), time.Now().UTC().Add(90*time.Second).Format(time.RFC3339Nano), stamp, id); e != nil {
		return nil, e
	}
	v := &contracts.DeepResumeProbeClaim{SchemaVersion: 1, ID: id, LeaseToken: token, Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task)), Attempt: attempt}, Origin: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task)), Attempt: origin}, AgentID: agent, SourcePath: t.SourcePath, OutputPath: t.OutputPath, SourceHash: source, ConfigurationHash: config, Model: t.Model, Identity: t.SDKIdentity}
	return v, tx.Commit()
}
func (s *Store) ReportDeepResumeCheck(ctx context.Context, v contracts.DeepResumeProbeReport) error {
	raw, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(contracts.KindDeepResumeProbeReport, raw, nil) != nil {
		return ErrDeepResume
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var task, attempt, origin int64
	var agent, lease, until, state, config, source string
	e = tx.QueryRowContext(ctx, `SELECT task_id,attempt,origin_attempt,agent_id,lease_hash,lease_until,state,configuration_hash,source_hash FROM deep_resume_checks WHERE id=? FOR UPDATE`, v.ID).Scan(&task, &attempt, &origin, &agent, &lease, &until, &state, &config, &source)
	if e != nil {
		return e
	}
	expected := deepDigest(v.LeaseToken)
	p := v.Inspection
	at, e := time.Parse(time.RFC3339Nano, until)
	if e != nil || time.Now().After(at) || state != "checking" || agent != v.AgentID || p.AgentID != agent || subtle.ConstantTimeCompare([]byte(expected), []byte(lease)) != 1 || p.Execution.TaskID != contracts.TaskID(fmt.Sprint(task)) || p.Execution.Attempt != attempt || p.Origin.TaskID != p.Execution.TaskID || p.Origin.Attempt != origin || p.ConfigurationHash != config || p.SourceHash != source {
		return ErrDeepResume
	}
	if p.Eligible && (p.ProofHash == nil || p.ScanID == nil || p.ThreadID == nil) {
		return ErrDeepResume
	}
	doc, _ := contracts.EncodeJSON(p)
	_, e = tx.ExecContext(ctx, `UPDATE deep_resume_checks SET state='ready',inspection_json=?,updated_at=? WHERE id=?`, string(doc), time.Now().UTC().Format(time.RFC3339Nano), v.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ResumeDeepScan(ctx context.Context, id int64, a contracts.DeepResumeAction) error {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()
	v, e := s.DeepResumeSnapshot(ctx, id)
	if e != nil {
		return e
	}
	if !v.CanResume || v.ProbeID == nil || *v.ProbeID != a.ProbeID || v.Inspection == nil || v.Inspection.ProofHash == nil || *v.Inspection.ProofHash != a.ProofHash || v.Execution != a.Execution || !a.AcknowledgeAdditionalCost {
		return ErrDeepResume
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var attempt int64
	var status string
	if e = tx.QueryRowContext(ctx, `SELECT attempt,status FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&attempt, &status); e != nil {
		return e
	}
	if attempt != a.Execution.Attempt || (status != "failed" && status != "timeout") {
		return ErrDeepResume
	}
	if _, e = s.checkBudgetAdmission(ctx, tx, id); e != nil {
		return e
	}
	if e = s.cleanupBlocksTask(ctx, tx, id); e != nil {
		return e
	}
	var checkState, raw, updated string
	if e = tx.QueryRowContext(ctx, `SELECT state,inspection_json,updated_at FROM deep_resume_checks WHERE id=? FOR UPDATE`, a.ProbeID).Scan(&checkState, &raw, &updated); e != nil {
		return e
	}
	stamp, _ := time.Parse(time.RFC3339Nano, updated)
	p, e := contracts.DecodeDeepResumeInspection([]byte(raw), nil)
	if e != nil || checkState != "ready" || time.Since(stamp) > 2*time.Minute || !p.Eligible || p.Execution != a.Execution || p.AgentID != v.Inspection.AgentID || p.ConfigurationHash != v.Inspection.ConfigurationHash || p.SourceHash != v.Inspection.SourceHash || p.ProofHash == nil || *p.ProofHash != a.ProofHash {
		return ErrDeepResume
	}
	var identityOK bool
	if e = tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM task_sdk_identities b WHERE b.task_id=?) OR EXISTS(SELECT 1 FROM task_sdk_identities b JOIN sdk_identities i ON i.id=b.identity_id WHERE b.task_id=? AND i.agent_id=? AND i.state='authenticated' AND i.authenticated=TRUE AND i.cleanup_pending=FALSE AND i.generation=(b.reference_json::jsonb->>'generation')::bigint)`, id, id, p.AgentID).Scan(&identityOK); e != nil {
		return e
	}
	if !identityOK || !s.deepAgentReady(ctx, p.AgentID) {
		return ErrDeepResume
	}
	out := contracts.DeepResumeAssignment{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: a.Execution.TaskID, Attempt: attempt + 1}, Previous: a.Execution, Inspection: p}
	doc, e := contracts.EncodeJSON(out)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO deep_resume_runs(task_id,attempt,previous_attempt,origin_attempt,agent_id,probe_id,document_json,status) VALUES(?,?,?,?,?,?,?,'pending') ON CONFLICT(task_id,attempt) DO UPDATE SET previous_attempt=excluded.previous_attempt,origin_attempt=excluded.origin_attempt,agent_id=excluded.agent_id,probe_id=excluded.probe_id,document_json=excluded.document_json,status='pending'`, id, attempt+1, attempt, p.Origin.Attempt, p.AgentID, a.ProbeID, string(doc))
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE deep_resume_checks SET state='used' WHERE id=?`, a.ProbeID); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET status='pending',queued_at=NOW(),automatic_retries=0,lease_expires_at=NULL,current_stage='',progress_json='',progress_diagnostic_json='',error_code=NULL,error_message=NULL,finished_at=NULL WHERE id=?`, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Bind a resumed result to its persisted assignment and reject cumulative cost
// accidentally reported as a fresh attempt's incremental estimate.
func (s *Store) validateDeepResumeResult(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	if !full.Result.Present {
		return nil
	}
	r := full.Result.Value
	raw, present := r.Extensions.Value["deepResume"]
	var expected string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM deep_resume_runs WHERE task_id=? AND attempt=? AND status<>'canceled'`, id, full.Execution.Attempt).Scan(&expected)
	if errors.Is(e, sql.ErrNoRows) {
		if present {
			return ErrInvalidResult
		}
		return nil
	}
	if e != nil {
		return e
	}
	if !present {
		return ErrInvalidResult
	}
	assigned, e := contracts.DecodeDeepResumeAssignment([]byte(expected), &full.Execution)
	if e != nil {
		return ErrInvalidResult
	}
	returned, e := contracts.DecodeDeepResumeAssignment(raw, &full.Execution)
	if e != nil || !sameJSON(assigned, returned) {
		return ErrInvalidResult
	}
	var native struct {
		ScanID   string `json:"scanId"`
		Manifest struct {
			Scan struct {
				ID string `json:"id"`
			} `json:"scan"`
		} `json:"manifest"`
		Cost *struct {
			EstimatedUSD *float64        `json:"estimatedUsd"`
			Range        json.RawMessage `json:"estimatedUsdRange"`
		} `json:"cost"`
	}
	if sdk, ok := r.Extensions.Value["sdk"]; ok {
		if json.Unmarshal(sdk, &native) != nil {
			return ErrInvalidResult
		}
	}
	scan := assigned.Inspection.ScanID
	if scan == nil || (native.ScanID != "" && native.ScanID != *scan) || (native.Manifest.Scan.ID != "" && native.Manifest.Scan.ID != *scan) {
		return ErrInvalidResult
	}
	if (r.Status == "completed" || r.Status == "partial") && native.ScanID == "" && native.Manifest.Scan.ID == "" {
		return ErrInvalidResult
	}
	if projection, e := contracts.NativeSDKResult(r); e == nil && projection != nil && projection.ScanID != nil && *projection.ScanID != *scan {
		return ErrInvalidResult
	}
	if r.Cost.Present && r.Cost.Value != nil && r.Cost.Value.EstimatedUSD != nil {
		baseline := assigned.Inspection.Baseline.EstimatedUSD
		if baseline == nil || native.Cost == nil || native.Cost.EstimatedUSD == nil {
			return ErrInvalidResult
		}
		cumulative := *native.Cost.EstimatedUSD
		if len(native.Cost.Range) > 0 {
			var bounds struct {
				Min *float64 `json:"min"`
				Max *float64 `json:"max"`
			}
			if json.Unmarshal(native.Cost.Range, &bounds) != nil || bounds.Min == nil || bounds.Max == nil || *bounds.Min != cumulative || *bounds.Max != cumulative {
				return ErrInvalidResult
			}
		}
		if cumulative < *baseline || math.Abs(*r.Cost.Value.EstimatedUSD-(cumulative-*baseline)) > 0.000001 {
			return ErrInvalidResult
		}
	}
	return nil
}
