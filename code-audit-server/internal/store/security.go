package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"time"
)

//go:embed security.sql
var securitySQL string

var ErrAuthenticationChanged = errors.New("account credentials or session changed")
var ErrAccountConflict = errors.New("account update conflicts with current state")

func securityTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z") }
func ValidRole(role string) bool      { return role == "admin" || role == "operator" || role == "viewer" }
func randomSecurityID() (string, error) {
	v := make([]byte, 24)
	_, e := rand.Read(v)
	return base64.RawURLEncoding.EncodeToString(v), e
}

type Session struct {
	ID          string `json:"id"`
	UserID      int64  `json:"-"`
	AuthVersion int64  `json:"-"`
	TokenHash   string `json:"-"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
	RevokedAt   string `json:"revokedAt"`
	Current     bool   `json:"current"`
}

func (s *Store) EnsureAdministrator(ctx context.Context, name, passwordHash string) (*User, error) {
	u, e := s.EnsureUser(ctx, name, passwordHash)
	if e != nil {
		return nil, e
	}
	if u.Role != "admin" {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `UPDATE users SET role='admin',auth_version=auth_version+1 WHERE id=? AND role<>'admin'`, u.ID); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at=''`, securityTime(time.Now()), u.ID); e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
	}
	return s.GetUser(ctx, u.ID)
}
func (s *Store) StartSession(ctx context.Context, user *User, expectedHash, newHash string, session Session) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	q := `SELECT password_hash,active,auth_version FROM users WHERE id=?`
	if s.db.postgres {
		q += ` FOR UPDATE`
	}
	var hash string
	var active bool
	var version int64
	if e = tx.QueryRowContext(ctx, q, user.ID).Scan(&hash, &active, &version); e != nil {
		return e
	}
	if !active || hash != expectedHash || version != user.AuthVersion {
		return ErrAuthenticationChanged
	}
	if newHash != "" {
		if _, e = tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, newHash, user.ID); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO auth_sessions(id,user_id,auth_version,token_hash,created_at,expires_at) VALUES(?,?,?,?,?,?)`, session.ID, user.ID, version, session.TokenHash, session.CreatedAt, session.ExpiresAt); e != nil {
		return e
	}
	// Keep ten active sessions per account; expiry/revocation is retained for
	// short-term diagnosis, while long-expired session secrets are purged.
	if _, e = tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at='' AND id NOT IN (SELECT id FROM auth_sessions WHERE user_id=? AND revoked_at='' ORDER BY created_at DESC,id DESC LIMIT 10)`, session.CreatedAt, user.ID, user.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at<?`, securityTime(time.Now().Add(-30*24*time.Hour))); e != nil {
		return e
	}
	auditID, e := randomSecurityID()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO operation_audits(id,actor_id,actor_name,action,resource,resource_id,outcome,occurred_at,completed_at) VALUES(?,?,?,'session.login','session',?,'succeeded',?,?)`, auditID, user.ID, user.Username, session.ID, session.CreatedAt, session.CreatedAt); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) SessionUser(ctx context.Context, id, hash string, uid, version int64, now time.Time) (*User, error) {
	var u User
	e := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.password_hash,u.created_at,u.role,u.active,u.auth_version FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.id=? AND a.token_hash=? AND a.user_id=? AND a.auth_version=? AND u.auth_version=a.auth_version AND u.active=1 AND a.revoked_at='' AND a.expires_at>?`, id, hash, uid, version, securityTime(now)).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.Role, &u.Active, &u.AuthVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAuthenticationChanged
	}
	return &u, e
}
func (s *Store) ListSessions(ctx context.Context, uid int64, current string) ([]Session, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,created_at,expires_at,revoked_at FROM auth_sessions WHERE user_id=? AND revoked_at='' AND expires_at>? ORDER BY created_at DESC,id LIMIT 50`, uid, securityTime(time.Now()))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var a Session
		if e = rows.Scan(&a.ID, &a.CreatedAt, &a.ExpiresAt, &a.RevokedAt); e != nil {
			return nil, e
		}
		a.Current = a.ID == current
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) RevokeSession(ctx context.Context, uid int64, id string) error {
	result, e := s.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at=''`, securityTime(time.Now()), id, uid)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n == 0 {
		return ErrAuthenticationChanged
	}
	return e
}
func (s *Store) ChangePassword(ctx context.Context, uid int64, expectedHash, newHash string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,auth_version=auth_version+1 WHERE id=? AND password_hash=? AND active=1`, newHash, uid, expectedHash)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrAuthenticationChanged
	}
	if _, e = tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at=''`, securityTime(time.Now()), uid); e != nil {
		return e
	}
	return tx.Commit()
}

type AuditEntry struct {
	ID          string `json:"id"`
	ActorID     *int64 `json:"actorId"`
	ActorName   string `json:"actorName"`
	Action      string `json:"action"`
	Resource    string `json:"resource"`
	ResourceID  string `json:"resourceId"`
	Outcome     string `json:"outcome"`
	OccurredAt  string `json:"occurredAt"`
	CompletedAt string `json:"completedAt"`
}

func (s *Store) BeginAudit(ctx context.Context, a AuditEntry) (string, error) {
	id, e := randomSecurityID()
	if e != nil {
		return "", e
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO operation_audits(id,actor_id,actor_name,action,resource,resource_id,outcome,occurred_at) VALUES(?,?,?,?,?,?,?,?)`, id, a.ActorID, a.ActorName, a.Action, a.Resource, a.ResourceID, a.Outcome, securityTime(time.Now()))
	return id, e
}
func (s *Store) FinishAudit(ctx context.Context, id, outcome, resourceID string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE operation_audits SET outcome=?,completed_at=?,resource_id=CASE WHEN ?<>'' THEN ? ELSE resource_id END WHERE id=?`, outcome, securityTime(time.Now()), resourceID, resourceID, id)
	return e
}
func (s *Store) ListAudits(ctx context.Context, before string) ([]AuditEntry, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,actor_id,actor_name,action,resource,resource_id,outcome,occurred_at,completed_at FROM operation_audits WHERE (?='' OR occurred_at<(SELECT occurred_at FROM operation_audits WHERE id=?) OR (occurred_at=(SELECT occurred_at FROM operation_audits WHERE id=?) AND id<?)) ORDER BY occurred_at DESC,id DESC LIMIT 100`, before, before, before, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var a AuditEntry
		if e = rows.Scan(&a.ID, &a.ActorID, &a.ActorName, &a.Action, &a.Resource, &a.ResourceID, &a.Outcome, &a.OccurredAt, &a.CompletedAt); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReserveLogin serializes each bucket with one conditional update. Requests
// rejected by the peer bucket never create arbitrary account buckets.
func (s *Store) ReserveLogin(ctx context.Context, buckets []string, limits []int, now time.Time, window time.Duration) (bool, int, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, 0, e
	}
	defer tx.Rollback()
	stamp, cutoff := securityTime(now), securityTime(now.Add(-window))
	if _, e = tx.ExecContext(ctx, `DELETE FROM login_limits WHERE window_start<?`, securityTime(now.Add(-24*time.Hour))); e != nil {
		return false, 0, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_limits`).Scan(&count); e != nil {
		return false, 0, e
	}
	if count >= 10000 {
		return false, 60, tx.Commit()
	}
	for i, bucket := range buckets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO login_limits(bucket,window_start,attempts) VALUES(?,?,0) ON CONFLICT(bucket) DO NOTHING`, bucket, stamp); e != nil {
			return false, 0, e
		}
		var started string
		e = tx.QueryRowContext(ctx, `UPDATE login_limits SET attempts=CASE WHEN window_start<=? THEN 1 ELSE attempts+1 END,window_start=CASE WHEN window_start<=? THEN ? ELSE window_start END WHERE bucket=? AND (window_start<=? OR attempts<?) RETURNING window_start`, cutoff, cutoff, stamp, bucket, cutoff, limits[i]).Scan(&started)
		if errors.Is(e, sql.ErrNoRows) {
			if e = tx.QueryRowContext(ctx, `SELECT window_start FROM login_limits WHERE bucket=?`, bucket).Scan(&started); e != nil {
				return false, 0, e
			}
			start, _ := time.Parse(time.RFC3339Nano, started)
			retry := max(1, int(start.Add(window).Sub(now).Seconds())+1)
			return false, retry, tx.Commit()
		}
		if e != nil {
			return false, 0, e
		}
	}
	return true, 0, tx.Commit()
}
func (s *Store) ClearLoginBucket(ctx context.Context, key string) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM login_limits WHERE bucket=?`, key)
	return e
}
func (s *Store) ListAccounts(ctx context.Context) ([]User, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,username,created_at,role,active,auth_version FROM users ORDER BY id LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if e = rows.Scan(&u.ID, &u.Username, &u.CreatedAt, &u.Role, &u.Active, &u.AuthVersion); e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) CreateAccount(ctx context.Context, name, hash, role string) (*User, error) {
	if !ValidRole(role) {
		return nil, ErrAccountConflict
	}
	var id int64
	e := s.db.QueryRowContext(ctx, `INSERT INTO users(username,password_hash,created_at,role,active,auth_version) VALUES(?,?,?,?,1,0) ON CONFLICT(username) DO NOTHING RETURNING id`, name, hash, securityTime(time.Now()), role).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAccountConflict
	}
	if e != nil {
		return nil, e
	}
	return s.GetUser(ctx, id)
}
func (s *Store) UpdateAccount(ctx context.Context, id int64, role string, active bool) error {
	if !ValidRole(role) {
		return ErrAccountConflict
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Serialize changes to the administrator set, so two requests cannot each
	// disable a different last administrator based on a stale count.
	if s.db.postgres {
		if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740920)`); e != nil {
			return e
		}
	}
	var previous string
	var enabled bool
	q := `SELECT role,active FROM users WHERE id=?`
	if s.db.postgres {
		q += ` FOR UPDATE`
	}
	if e = tx.QueryRowContext(ctx, q, id).Scan(&previous, &enabled); e != nil {
		return e
	}
	if previous == "admin" && enabled && (role != "admin" || !active) {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1`).Scan(&count); e != nil {
			return e
		}
		if count <= 1 {
			return ErrAccountConflict
		}
	}
	if previous == role && enabled == active {
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE users SET role=?,active=?,auth_version=auth_version+1 WHERE id=?`, role, boolInt(active), id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at=''`, securityTime(time.Now()), id); e != nil {
		return e
	}
	return tx.Commit()
}
