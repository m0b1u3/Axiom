package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"time"
)

//go:embed storage.sql
var storageSQL string

const maxStorageBytes int64 = 9007199254740991

var ErrStorageConflict = errors.New("清理计划或存储策略已变化，请刷新预览")
var ErrStorageProtected = errors.New("数据仍被任务引用、费用未知或已有清理记录，已保留；请先完成任务或核对费用")

// The exclusive session lock covers filesystem operations and their durable
// journal. Admission/retry transactions and uploads take the shared lock.
func (s *Store) storageGate(ctx context.Context, exclusive bool) (func(), error) {
	if !s.db.postgres {
		if exclusive {
			s.storageMutex.Lock()
			return s.storageMutex.Unlock, nil
		}
		s.storageMutex.RLock()
		return s.storageMutex.RUnlock, nil
	}
	// Gate waiters use a separate pool: they must never exhaust the transaction
	// connections needed by the exclusive filesystem worker to finish its journal.
	conn, e := s.storageGateDB.Conn(ctx)
	if e != nil {
		return nil, e
	}
	lock, unlock := "SELECT pg_advisory_lock_shared(740920)", "SELECT pg_advisory_unlock_shared(740920)"
	if exclusive {
		lock, unlock = "SELECT pg_advisory_lock(740920)", "SELECT pg_advisory_unlock(740920)"
	}
	if _, e = conn.ExecContext(ctx, lock); e != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return nil, e
	}
	return func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released bool
		if e := conn.QueryRowContext(release, unlock).Scan(&released); e != nil || !released {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
	}, nil
}
func (s *Store) StorageLock(ctx context.Context, exclusive bool, fn func() error) error {
	release, e := s.storageGate(ctx, exclusive)
	if e != nil {
		return e
	}
	defer release()
	return fn()
}
func (s *Store) StoragePolicy(ctx context.Context) (contracts.StoragePolicy, error) {
	p := contracts.StoragePolicy{Revision: 1, MinFreeBytes: 1 << 30, OrphanGraceHours: 24}
	e := s.db.QueryRowContext(ctx, `SELECT revision,min_free_bytes,orphan_grace_hours FROM storage_settings WHERE id=1`).Scan(&p.Revision, &p.MinFreeBytes, &p.OrphanGraceHours)
	if errors.Is(e, sql.ErrNoRows) {
		return p, nil
	}
	return p, e
}
func (s *Store) UpdateStoragePolicy(ctx context.Context, p contracts.StoragePolicy) error {
	if p.Revision < 1 || p.MinFreeBytes < 0 || p.MinFreeBytes > maxStorageBytes || p.OrphanGraceHours < 1 || p.OrphanGraceHours > 8760 {
		return ErrStorageConflict
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO storage_settings(id,revision,min_free_bytes,orphan_grace_hours) VALUES(1,1,1073741824,24) ON CONFLICT DO NOTHING`); e != nil {
		return e
	}
	r, e := tx.ExecContext(ctx, `UPDATE storage_settings SET revision=revision+1,min_free_bytes=?,orphan_grace_hours=? WHERE id=1 AND revision=?`, p.MinFreeBytes, p.OrphanGraceHours, p.Revision)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrStorageConflict
	}
	return tx.Commit()
}

type StorageReference struct {
	Path, Reason, Kind string
	ID                 int64
}

func (s *Store) StorageReferences(ctx context.Context) ([]StorageReference, error) {
	out := []StorageReference{}
	rows, e := s.db.QueryContext(ctx, `SELECT t.id,t.status,COALESCE(t.output_path,''),t.source_path,EXISTS(SELECT 1 FROM task_cost_attempts c WHERE c.task_id=t.id AND c.state<>'complete'),(EXISTS(SELECT 1 FROM sdk_operations o WHERE o.origin_task_id=t.id) OR EXISTS(SELECT 1 FROM component_operations o WHERE o.task_id=t.id)) FROM scan_tasks t`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		var status, path, source string
		var unknown, linked bool
		if e = rows.Scan(&id, &status, &path, &source, &unknown, &linked); e != nil {
			rows.Close()
			return nil, e
		}
		reason := "historical_report"
		if IsRunningStatus(status) {
			reason = "running_task"
		} else if unknown {
			reason = "unknown_cost"
		} else if linked {
			reason = "sdk_operation_source"
		}
		out = append(out, StorageReference{path, reason, "scan", id}, StorageReference{source, "source_reference", "scan", id})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if s.db.postgres {
		rows, e = s.db.QueryContext(ctx, `SELECT a.task_id,COALESCE(a.output_path,''),t.status,EXISTS(SELECT 1 FROM task_cost_attempts c WHERE c.task_id=t.id AND c.state<>'complete') FROM scan_attempts a JOIN scan_tasks t ON t.id=a.task_id`)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id int64
			var path, status string
			var unknown bool
			if e = rows.Scan(&id, &path, &status, &unknown); e != nil {
				rows.Close()
				return nil, e
			}
			reason := "historical_report"
			if IsRunningStatus(status) {
				reason = "running_task"
			} else if unknown {
				reason = "unknown_cost"
			}
			out = append(out, StorageReference{path, reason, "scan", id})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	rows, e = s.db.QueryContext(ctx, `SELECT id,snapshot_path FROM source_snapshots`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var r StorageReference
		r.Kind = "snapshot"
		r.Reason = "snapshot_record"
		if e = rows.Scan(&r.ID, &r.Path); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	return out, e
}
func (s *Store) CleanupResourcePaths(ctx context.Context, kind string, id int64, jobID string) ([]string, error) {
	var busy int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM storage_cleanup_jobs WHERE kind=? AND resource_id=? AND state IN ('running','failed') AND id<>?`, kind, id, jobID).Scan(&busy); e != nil {
		return nil, e
	}
	if busy > 0 {
		return nil, ErrStorageProtected
	}
	if kind == "scan" {
		var resumeBusy int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deep_resume_checks WHERE task_id=? AND ((state='queued' AND updated_at>=?) OR (state='checking' AND lease_until>=?) OR (state='ready' AND updated_at>=? AND inspection_json LIKE '%"eligible":true%'))`, id, time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339Nano)).Scan(&resumeBusy); e != nil {
			return nil, e
		}
		if resumeBusy > 0 {
			return nil, ErrStorageProtected
		}
		var related int
		relatedQuery := `SELECT (SELECT COUNT(*) FROM sdk_operations WHERE origin_task_id=?)+(SELECT COUNT(*) FROM component_operations WHERE task_id=?)`
		relatedArgs := []any{id, id}
		if s.IsPostgres() {
			relatedQuery += `+(SELECT COUNT(*) FROM interim_reviews WHERE task_id=?)`
			relatedArgs = append(relatedArgs, id)
		}
		if e := s.db.QueryRowContext(ctx, relatedQuery, relatedArgs...).Scan(&related); e != nil {
			return nil, e
		}
		if related > 0 {
			return nil, ErrStorageProtected
		}
		t, e := s.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		if !IsTerminalStatus(t.Status) {
			return nil, ErrStorageProtected
		}
		var unknown int
		if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_cost_attempts WHERE task_id=? AND state<>'complete'`, id).Scan(&unknown); e != nil {
			return nil, e
		}
		if unknown > 0 {
			return nil, ErrStorageProtected
		}
		a, e := s.ListAttempts(ctx, id)
		if e != nil {
			return nil, e
		}
		paths := []string{t.OutputPath}
		for _, v := range a {
			paths = append(paths, v.OutputPath)
		}
		return paths, nil
	}
	if kind == "snapshot" {
		v, e := s.GetSnapshot(ctx, id)
		if e != nil {
			return nil, e
		}
		var count int
		if e = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM scan_tasks WHERE source_snapshot_id=? OR source_path=?)+(SELECT COUNT(*) FROM component_plans WHERE snapshot_id=?)`, id, v.SnapshotPath, id).Scan(&count); e != nil {
			return nil, e
		}
		if count > 0 {
			return nil, ErrStorageProtected
		}
		return []string{v.SnapshotPath}, nil
	}
	return nil, ErrStorageConflict
}
func (s *Store) SaveCleanupJob(ctx context.Context, j contracts.CleanupJob, roots string, create bool) error {
	raw, e := contracts.EncodeJSON(j)
	if e != nil {
		return e
	}
	if e = contracts.ValidateJSON(contracts.KindCleanupJob, raw, nil); e != nil {
		return e
	}
	var id *int64
	if j.ResourceID != nil {
		v, e := strconv.ParseInt(string(*j.ResourceID), 10, 64)
		if e != nil {
			return e
		}
		id = &v
	}
	if create {
		_, e = s.db.ExecContext(ctx, `INSERT INTO storage_cleanup_jobs(id,kind,resource_id,state,document_json,roots_json,updated_at) VALUES(?,?,?,?,?,?,?)`, j.ID, j.Kind, id, j.State, string(raw), roots, j.UpdatedAt)
		return e
	}
	r, e := s.db.ExecContext(ctx, `UPDATE storage_cleanup_jobs SET state=?,document_json=?,updated_at=? WHERE id=?`, j.State, string(raw), j.UpdatedAt, j.ID)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) CleanupJob(ctx context.Context, id string) (contracts.CleanupJob, string, error) {
	var raw, roots string
	e := s.db.QueryRowContext(ctx, `SELECT document_json,roots_json FROM storage_cleanup_jobs WHERE id=?`, id).Scan(&raw, &roots)
	if e != nil {
		return contracts.CleanupJob{}, "", e
	}
	v, e := contracts.DecodeCleanupJob([]byte(raw), nil)
	return v, roots, e
}
func (s *Store) CleanupJobs(ctx context.Context, active bool) ([]contracts.CleanupJob, error) {
	q := `SELECT document_json FROM storage_cleanup_jobs`
	if active {
		q += ` WHERE state IN ('running','failed')`
	}
	q += ` ORDER BY updated_at DESC,id`
	if !active {
		q += ` LIMIT 100`
	}
	rows, e := s.db.QueryContext(ctx, q)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []contracts.CleanupJob{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		j, e := contracts.DecodeCleanupJob([]byte(raw), nil)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Called only after all approved filesystem items are durably purged. The same
// transaction finishes the journal and metadata deletion; failed purges retain both.
func (s *Store) FinishCleanup(ctx context.Context, j contracts.CleanupJob) error {
	for _, i := range j.Items {
		if i.State != "purged" && i.State != "protected" {
			return ErrStorageConflict
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if j.ResourceID != nil {
		id, e := strconv.ParseInt(string(*j.ResourceID), 10, 64)
		if e != nil {
			return e
		}
		if j.Kind == "scan" {
			if _, e = tx.ExecContext(ctx, `DELETE FROM findings WHERE scan_id=?`, id); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `DELETE FROM scan_tasks WHERE id=?`, id)
		} else if j.Kind == "snapshot" {
			_, e = tx.ExecContext(ctx, `DELETE FROM source_snapshots WHERE id=?`, id)
		}
		if e != nil {
			return e
		}
	}
	j.State = "completed"
	j.Error = ""
	j.Revision++
	j.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
	raw, e := contracts.EncodeJSON(j)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE storage_cleanup_jobs SET state='completed',document_json=?,updated_at=? WHERE id=?`, string(raw), j.UpdatedAt, j.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) cleanupBlocksTask(ctx context.Context, tx *transaction, id int64) error {
	var n int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM storage_cleanup_jobs WHERE kind='scan' AND resource_id=? AND state IN ('running','failed')`, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return fmt.Errorf("%w：任务正在清理，不能重试", ErrStorageProtected)
	}
	return nil
}
