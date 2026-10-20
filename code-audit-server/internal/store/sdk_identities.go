package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrSDKIdentity = errors.New("SDK 身份状态已变化、节点不匹配或仍有执行，请刷新后重试")

func identityStamp() string { return time.Now().UTC().Format(time.RFC3339) }
func identityTx(ctx context.Context, s *Store) (*transaction, error) {
	if !s.IsPostgres() {
		return nil, errors.New("SDK 执行身份需要 PostgreSQL；历史任务仍可读取")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740919)`); e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func expireIdentityOps(ctx context.Context, tx *transaction) error {
	stamp := identityStamp()
	_, e := tx.ExecContext(ctx, `UPDATE sdk_identities i SET state=CASE WHEN i.state='revoked' THEN i.state ELSE 'unknown' END,authenticated=NULL,updated_at=? FROM sdk_identity_operations o WHERE o.identity_id=i.id AND o.status IN ('queued','running','waiting') AND (o.deadline<? OR (o.status IN ('running','waiting') AND o.lease_until<?))`, stamp, stamp, stamp)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE sdk_identity_operations SET status='expired',secret_encrypted='',instructions_json=NULL,updated_at=?,error='操作超时或节点失联；不会自动重新登录，请在 Web 核对身份' WHERE status IN ('queued','running','waiting') AND (deadline<? OR (status IN ('running','waiting') AND lease_until<?))`, stamp, stamp, stamp)
	return e
}
func scanIdentity(row interface{ Scan(...any) error }) (contracts.SDKIdentitySnapshot, error) {
	var v contracts.SDKIdentitySnapshot
	var id int64
	v.SchemaVersion = 1
	e := row.Scan(&id, &v.Identity.Name, &v.Identity.AgentID, &v.Identity.Kind, &v.Identity.Generation, &v.State, &v.Authenticated, &v.CheckedAt, &v.CreatedAt, &v.UpdatedAt, &v.CleanupPending)
	v.Identity.ID = fmt.Sprint(id)
	return v, e
}

const identityColumns = `id,name,agent_id,kind,generation,state,authenticated,checked_at,created_at,updated_at,cleanup_pending`

func (s *Store) CreateSDKIdentity(ctx context.Context, name, agent, kind string) (contracts.SDKIdentitySnapshot, error) {
	ref := contracts.SDKIdentityRef{ID: "1", Name: name, AgentID: agent, Kind: contracts.SDKIdentityRefKind(kind), Generation: 1}
	raw, _ := contracts.EncodeJSON(ref)
	if contracts.ValidateJSON(contracts.KindSDKIdentityRef, raw, nil) != nil {
		return contracts.SDKIdentitySnapshot{}, ErrSDKIdentity
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return contracts.SDKIdentitySnapshot{}, e
	}
	defer tx.Rollback()
	stamp := identityStamp()
	v, e := scanIdentity(tx.QueryRowContext(ctx, `INSERT INTO sdk_identities(name,agent_id,kind,created_at,updated_at) VALUES(?,?,?,?,?) RETURNING `+identityColumns, name, agent, kind, stamp, stamp))
	if e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func (s *Store) SDKIdentities(ctx context.Context) ([]contracts.SDKIdentitySnapshot, error) {
	if !s.IsPostgres() {
		return []contracts.SDKIdentitySnapshot{}, nil
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = expireIdentityOps(ctx, tx); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+identityColumns+` FROM sdk_identities ORDER BY id DESC LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	out := []contracts.SDKIdentitySnapshot{}
	for rows.Next() {
		v, e := scanIdentity(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) SDKIdentity(ctx context.Context, id int64) (contracts.SDKIdentitySnapshot, error) {
	return scanIdentity(s.db.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM sdk_identities WHERE id=?`, id))
}
func scanIdentityOperation(row interface{ Scan(...any) error }) (contracts.SDKIdentityOperation, error) {
	var v contracts.SDKIdentityOperation
	var id int64
	var ref string
	var instructions *string
	v.SchemaVersion = 1
	e := row.Scan(&id, &ref, &v.Action, &v.Status, &v.CreatedAt, &v.UpdatedAt, &v.Deadline, &instructions, &v.Error)
	v.ID = fmt.Sprint(id)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal([]byte(ref), &v.Identity)
	if e == nil && instructions != nil {
		e = json.Unmarshal([]byte(*instructions), &v.Instructions)
	}
	return v, e
}

const identityOperationColumns = `id,reference_json,action,status,created_at,updated_at,deadline,instructions_json,error`

func (s *Store) SDKIdentityOperations(ctx context.Context, id int64) ([]contracts.SDKIdentityOperation, error) {
	if !s.IsPostgres() {
		return nil, ErrSDKIdentity
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = expireIdentityOps(ctx, tx); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+identityOperationColumns+` FROM sdk_identity_operations WHERE identity_id=? ORDER BY id DESC LIMIT 100`, id)
	if e != nil {
		return nil, e
	}
	out := []contracts.SDKIdentityOperation{}
	for rows.Next() {
		v, e := scanIdentityOperation(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) NewSDKIdentityOperation(ctx context.Context, id, generation int64, action, encrypted string) (contracts.SDKIdentityOperation, error) {
	var empty contracts.SDKIdentityOperation
	tx, e := identityTx(ctx, s)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	if e = expireIdentityOps(ctx, tx); e != nil {
		return empty, e
	}
	v, e := scanIdentity(tx.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM sdk_identities WHERE id=? FOR UPDATE`, id))
	if e != nil {
		return empty, e
	}
	if v.State == "revoked" && !v.CleanupPending && action == "logout" {
		return empty, ErrSDKIdentity
	}
	if v.Identity.Generation != generation {
		return empty, ErrSDKIdentity
	}
	if action != "login_api_key" && action != "login_device_code" && action != "account" && action != "logout" {
		return empty, ErrSDKIdentity
	}
	if (action == "login_api_key") != (encrypted != "") || (action == "login_api_key" && v.Identity.Kind != "api_key") || (action == "login_device_code" && v.Identity.Kind != "chatgpt") {
		return empty, ErrSDKIdentity
	}
	stamp := identityStamp()
	ref := v.Identity
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM sdk_identity_operations WHERE identity_id=? AND status IN ('queued','running','waiting')`, id).Scan(&active); e != nil {
		return empty, e
	}
	if action != "logout" {
		if active > 0 || v.CleanupPending || v.State == "revoked" {
			return empty, ErrSDKIdentity
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM scan_tasks t JOIN task_sdk_identities b ON b.task_id=t.id WHERE b.identity_id=? AND t.status IN ('claimed','scanning','cancel_requested') AND t.lease_expires_at>=?`, id, stamp).Scan(&active); e != nil {
			return empty, e
		}
		if active > 0 {
			return empty, ErrSDKIdentity
		}
		if action != "account" {
			ref.Generation++
			if _, e = tx.ExecContext(ctx, `UPDATE sdk_identities SET generation=?,state='authenticating',authenticated=NULL,updated_at=? WHERE id=?`, ref.Generation, stamp, id); e != nil {
				return empty, e
			}
		}
	} else {
		if v.State == "revoked" {
			var previous string
			e = tx.QueryRowContext(ctx, `SELECT reference_json FROM sdk_identity_operations WHERE identity_id=? AND action='logout' ORDER BY id DESC LIMIT 1`, id).Scan(&previous)
			if e == nil {
				json.Unmarshal([]byte(previous), &ref)
			} else if !errors.Is(e, sql.ErrNoRows) {
				return empty, e
			}
		} else {
			if _, e = tx.ExecContext(ctx, `UPDATE sdk_identities SET generation=generation+1,state='revoked',authenticated=NULL,cleanup_pending=TRUE,updated_at=? WHERE id=?`, stamp, id); e != nil {
				return empty, e
			}
		}
		// Revocation is effective in the same admission transaction; running work is
		// cancelled before an Agent may remove its private credential directory.
		if _, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET status='cancel_requested' WHERE id IN (SELECT task_id FROM task_sdk_identities WHERE identity_id=?) AND status IN ('claimed','scanning')`, id); e != nil {
			return empty, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE sdk_identity_operations SET status='canceled',secret_encrypted='',instructions_json=NULL,updated_at=? WHERE identity_id=? AND status IN ('queued','running','waiting')`, stamp, id); e != nil {
			return empty, e
		}
	}
	raw, _ := contracts.EncodeJSON(ref)
	op, e := scanIdentityOperation(tx.QueryRowContext(ctx, `INSERT INTO sdk_identity_operations(identity_id,reference_json,action,secret_encrypted,created_at,updated_at,deadline) VALUES(?,?,?,?,?,?,?) RETURNING `+identityOperationColumns, id, string(raw), action, encrypted, stamp, stamp, time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339)))
	if e != nil {
		return empty, e
	}
	return op, tx.Commit()
}
func (s *Store) CancelSDKIdentityOperation(ctx context.Context, id int64) error {
	tx, e := identityTx(ctx, s)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	stamp := identityStamp()
	var identity int64
	e = tx.QueryRowContext(ctx, `UPDATE sdk_identity_operations SET status='canceled',secret_encrypted='',instructions_json=NULL,updated_at=? WHERE id=? AND action<>'logout' AND status IN ('queued','running','waiting') RETURNING identity_id`, stamp, id).Scan(&identity)
	if e != nil {
		return ErrSDKIdentity
	}
	if _, e = tx.ExecContext(ctx, `UPDATE sdk_identities SET state=CASE WHEN state='revoked' THEN state ELSE 'unknown' END,authenticated=NULL,updated_at=? WHERE id=?`, stamp, identity); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ClaimSDKIdentity(ctx context.Context, agent string, limits ClaimLimits) (*contracts.SDKIdentityClaim, string, error) {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return nil, "", e
	}
	defer release()
	tx, e := identityTx(ctx, s)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	if e = expireIdentityOps(ctx, tx); e != nil {
		return nil, "", e
	}
	// Identity credentials require an observed, matching runtime. Requesting the
	// capability alone must never deliver a secret to an old or blocked Scanner.
	var runtimeJSON string
	var seen *time.Time
	e = tx.QueryRowContext(ctx, `SELECT runtime_json,runtime_seen_at FROM execution_agents WHERE agent_id=?`, agent).Scan(&runtimeJSON, &seen)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, "", e
	}
	if seen == nil || time.Since(*seen) >= 30*time.Second {
		return nil, "", tx.Commit()
	}
	runtime, e := contracts.DecodeAgentRuntime([]byte(runtimeJSON), nil)
	if e != nil || runtime.State != contracts.AgentRuntimeStateReady || runtime.ControlAPI != 4 || runtime.MatrixHash != control.MatrixHash || runtime.ScannerDescriptionVersion == nil || !contracts.DescriptionSupportsSDKIdentity(*runtime.ScannerDescriptionVersion) {
		return nil, "", tx.Commit()
	}
	if limits.StorageCheck != nil && limits.StorageCheck(ctx) != "" {
		return nil, "", tx.Commit()
	}
	stamp := identityStamp()
	row := tx.QueryRowContext(ctx, `SELECT `+identityOperationColumns+` FROM sdk_identity_operations o WHERE status='queued' AND reference_json::jsonb->>'agentId'=? AND NOT EXISTS(SELECT 1 FROM scan_tasks t JOIN task_sdk_identities b ON b.task_id=t.id WHERE b.identity_id=o.identity_id AND t.status IN ('claimed','scanning','cancel_requested') AND t.lease_expires_at>=?) AND NOT EXISTS(SELECT 1 FROM sdk_identity_operations old WHERE old.identity_id=o.identity_id AND old.id<>o.id AND old.status='canceled' AND old.lease_until>=?) ORDER BY id LIMIT 1 FOR UPDATE`, agent, stamp, stamp)
	op, e := scanIdentityOperation(row)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, "", tx.Commit()
	}
	if e != nil {
		return nil, "", e
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		return nil, "", e
	}
	token := hex.EncodeToString(b)
	hash := sha256.Sum256([]byte(token))
	var encrypted string
	e = tx.QueryRowContext(ctx, `UPDATE sdk_identity_operations SET status='running',lease_hash=?,lease_until=?,updated_at=? WHERE id=? RETURNING secret_encrypted`, hex.EncodeToString(hash[:]), time.Now().UTC().Add(limits.Lease).Format(time.RFC3339), stamp, op.ID).Scan(&encrypted)
	if e != nil {
		return nil, "", e
	}
	op.Status = "running"
	op.UpdatedAt = stamp
	claim := &contracts.SDKIdentityClaim{SchemaVersion: 1, Operation: op, LeaseToken: token, LeaseSeconds: int64(limits.Lease / time.Second)}
	return claim, encrypted, tx.Commit()
}
func (s *Store) ReportSDKIdentity(ctx context.Context, v contracts.SDKIdentityReport, lease time.Duration) (string, error) {
	raw, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(contracts.KindSDKIdentityReport, raw, nil) != nil {
		return "", ErrSDKIdentity
	}
	tx, e := identityTx(ctx, s)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	op, e := scanIdentityOperation(tx.QueryRowContext(ctx, `SELECT `+identityOperationColumns+` FROM sdk_identity_operations WHERE id=? FOR UPDATE`, v.OperationID))
	if e != nil {
		return "", e
	}
	var token, until, lastHash string
	var seq int64
	e = tx.QueryRowContext(ctx, `SELECT lease_hash,COALESCE(lease_until,''),sequence,report_hash FROM sdk_identity_operations WHERE id=?`, v.OperationID).Scan(&token, &until, &seq, &lastHash)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256([]byte(v.LeaseToken))
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	stamp := identityStamp()
	if token != hex.EncodeToString(sum[:]) || op.Identity.AgentID != v.AgentID || op.Identity.ID != v.IdentityID || op.Identity.Generation != v.Generation {
		return "", ErrSDKIdentity
	}
	if v.Sequence == seq && hash == lastHash && op.Status == "completed" {
		return "completed", tx.Commit()
	}
	if op.Status == "completed" {
		return "", ErrSDKIdentity
	}
	if op.Status != "running" && op.Status != "waiting" {
		if op.Status == "canceled" && v.Status == "canceled" {
			if _, e = tx.ExecContext(ctx, `UPDATE sdk_identity_operations SET lease_until=NULL WHERE id=?`, op.ID); e != nil {
				return "", e
			}
		}
		return string(op.Status), tx.Commit()
	}
	if until < stamp || op.Deadline < stamp {
		return "expired", ErrLeaseLost
	}
	if v.Sequence < seq || (v.Sequence == seq && hash != lastHash) || v.Sequence > seq+1 {
		return "", ErrSDKIdentity
	}
	if v.Status == "waiting" && (op.Action != "login_device_code" || v.Instructions == nil) {
		return "", ErrSDKIdentity
	}
	if v.Status != "waiting" && v.Instructions != nil {
		return "", ErrSDKIdentity
	}
	if v.Status == "completed" && v.Authenticated == nil {
		return "", ErrSDKIdentity
	}
	if v.Status == "completed" && op.Action == "logout" && *v.Authenticated {
		return "", ErrSDKIdentity
	}
	if v.Status == "completed" && (op.Action == "login_api_key" || op.Action == "login_device_code") && !*v.Authenticated {
		return "", ErrSDKIdentity
	}
	var instructions any
	if v.Instructions != nil {
		if v.Instructions.ExpiresAt != op.Deadline {
			return "", ErrSDKIdentity
		}
		b, _ := json.Marshal(v.Instructions)
		instructions = string(b)
	}
	terminal := v.Status == "completed" || v.Status == "failed" || v.Status == "canceled"
	// Raw SDK stdout/details are intentionally never a public account summary.
	var message any
	if v.Error != nil {
		message = "SDK 身份操作未完成；请检查节点并在 Web 重新核对"
	}
	_, e = tx.ExecContext(ctx, `UPDATE sdk_identity_operations SET status=?,sequence=?,report_hash=?,instructions_json=?,error=?,updated_at=?,lease_until=?,secret_encrypted=CASE WHEN ? THEN '' ELSE secret_encrypted END WHERE id=?`, string(v.Status), v.Sequence, hash, instructions, message, stamp, time.Now().UTC().Add(lease).Format(time.RFC3339), terminal, op.ID)
	if e != nil {
		return "", e
	}
	if terminal {
		if op.Action == "logout" {
			if v.Status == "completed" {
				_, e = tx.ExecContext(ctx, `UPDATE sdk_identities SET cleanup_pending=FALSE,updated_at=? WHERE id=? AND state='revoked'`, stamp, v.IdentityID)
			}
		} else {
			state := "unknown"
			if v.Status == "completed" {
				state = "signed_out"
				if *v.Authenticated {
					state = "authenticated"
				}
			}
			_, e = tx.ExecContext(ctx, `UPDATE sdk_identities SET state=?,authenticated=?,checked_at=?,updated_at=? WHERE id=? AND generation=? AND state<>'revoked'`, state, v.Authenticated, stamp, stamp, v.IdentityID, v.Generation)
		}
		if e != nil {
			return "", e
		}
	}
	return string(v.Status), tx.Commit()
}
func (s *Store) saveTaskIdentity(ctx context.Context, tx *transaction, t Task, id int64) error {
	if t.SDKIdentity == nil {
		return nil
	}
	var model contracts.ResolvedModel
	if json.Unmarshal([]byte(t.ModelSnapshotJSON), &model) != nil || model.ProviderID != "openai" || model.ConnectionType != "openai" || (model.BaseURL.Present && model.BaseURL.Value != "https://api.openai.com/v1") || model.WireAPI != "responses" || t.SourceSnapshotID <= 0 || t.CredentialSnapshotEncrypted != "" {
		return ErrSDKIdentity
	}
	if !s.IsPostgres() {
		return ErrSDKIdentity
	}
	raw, e := contracts.EncodeJSON(t.SDKIdentity)
	if e != nil || contracts.ValidateJSON(contracts.KindSDKIdentityRef, raw, nil) != nil {
		return ErrSDKIdentity
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sdk_identities WHERE id=? AND generation=? AND agent_id=? AND kind=? AND state='authenticated' AND authenticated=TRUE AND cleanup_pending=FALSE)`, t.SDKIdentity.ID, t.SDKIdentity.Generation, t.SDKIdentity.AgentID, t.SDKIdentity.Kind).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return ErrSDKIdentity
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO task_sdk_identities(task_id,identity_id,reference_json) VALUES(?,?,?)`, id, t.SDKIdentity.ID, string(raw))
	return e
}
func (s *Store) loadTaskIdentity(ctx context.Context, t *Task) error {
	if !s.IsPostgres() {
		return nil
	}
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT reference_json FROM task_sdk_identities WHERE task_id=?`, t.ID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	v, e := contracts.DecodeSDKIdentityRef([]byte(raw), nil)
	if e == nil {
		t.SDKIdentity = &v
	}
	return e
}

func (s *Store) validateIdentityResult(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	t := Task{ID: id}
	if e := s.loadTaskIdentity(ctx, &t); e != nil {
		return e
	}
	if !full.Result.Present {
		return nil
	}
	raw, exists := full.Result.Value.Extensions.Value["sdkIdentity"]
	if t.SDKIdentity == nil {
		if exists {
			return ErrInvalidResult
		}
		return nil
	}
	if !exists {
		return ErrInvalidResult
	}
	v, e := contracts.DecodeSDKTaskIdentity(raw, &full.Execution)
	if e != nil || !sameJSON(v.Identity, *t.SDKIdentity) {
		return ErrInvalidResult
	}
	return nil
}
