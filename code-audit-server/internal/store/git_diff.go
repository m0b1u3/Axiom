package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"
)

//go:embed git_diff.sql
var gitDiffSQL string

func (s *Store) GitSnapshotTarget(ctx context.Context, id int64) (*contracts.GitDiffTarget, error) {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT target_json FROM git_snapshots WHERE snapshot_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := contracts.DecodeGitDiffTarget([]byte(raw), nil)
	return &v, e
}
func (s *Store) TaskGitTarget(ctx context.Context, id int64) (*contracts.GitDiffTarget, error) {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT target_json FROM task_git_targets WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := contracts.DecodeGitDiffTarget([]byte(raw), nil)
	return &v, e
}
func (s *Store) RegisterGitSnapshot(ctx context.Context, row SnapshotRow, target contracts.GitDiffTarget, uploadedHash string) (*SnapshotRow, error) {
	raw, e := contracts.EncodeJSON(target)
	if e != nil {
		return nil, e
	}
	if _, e = contracts.DecodeGitDiffTarget(raw, nil); e != nil {
		return nil, e
	}
	// Asset and Git identity become visible in the same transaction. A failed
	// registration must never turn a diff snapshot into an ordinary scan source.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var id int64
	e = tx.QueryRowContext(ctx, `INSERT INTO source_snapshots(original_filename,archive_sha256,content_hash,snapshot_path,file_count,total_size,project_id,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(archive_sha256) DO NOTHING RETURNING id`, row.OriginalFilename, row.ArchiveSha256, row.ContentHash, row.SnapshotPath, row.FileCount, row.TotalSize, nullIfEmptyZero(row.ProjectID), time.Now().UTC().Format(time.RFC3339)).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		var saved string
		if e = tx.QueryRowContext(ctx, `SELECT s.id,g.target_json FROM source_snapshots s JOIN git_snapshots g ON g.snapshot_id=s.id WHERE s.archive_sha256=?`, row.ArchiveSha256).Scan(&id, &saved); e != nil {
			return nil, e
		}
		v, e := contracts.DecodeGitDiffTarget([]byte(saved), nil)
		if e != nil || !sameJSON(v, target) {
			return nil, errors.New("Git 快照目标与已有不可变记录不一致")
		}
	} else if e != nil {
		return nil, e
	} else {
		if _, e = tx.ExecContext(ctx, `INSERT INTO git_snapshots(snapshot_id,uploaded_archive_sha256,target_json) VALUES(?,?,?)`, id, uploadedHash, string(raw)); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.GetSnapshot(ctx, id)
}
func (s *Store) saveGitTarget(ctx context.Context, tx *transaction, t Task, id int64) error {
	if t.GitTarget == nil {
		return nil
	}
	var raw, hash, path string
	if e := tx.QueryRowContext(ctx, `SELECT g.target_json,s.content_hash,s.snapshot_path FROM git_snapshots g JOIN source_snapshots s ON s.id=g.snapshot_id WHERE g.snapshot_id=?`, t.SourceSnapshotID).Scan(&raw, &hash, &path); e != nil {
		return e
	}
	target, e := contracts.DecodeGitDiffTarget([]byte(raw), nil)
	if e != nil || !sameJSON(target, *t.GitTarget) || hash != t.SourceContentHash || path != t.SourcePath {
		return errors.New("Git 目标必须来自冻结的 Git 快照")
	}
	encoded, e := contracts.EncodeJSON(t.GitTarget)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO task_git_targets(task_id,target_json) VALUES(?,?)`, id, string(encoded))
	return e
}
func (s *Store) validateGitCompletion(ctx context.Context, id int64, full contracts.ExecutionCompletion) error {
	target, e := s.TaskGitTarget(ctx, id)
	if e != nil {
		return e
	}
	if target == nil {
		if full.Result.Present {
			result := full.Result.Value
			if result.Extensions.Present && result.Extensions.Value["gitDiff"] != nil {
				return ErrInvalidResult
			}
			for _, f := range result.Findings {
				if f.Extensions.Present && f.Extensions.Value["axiomGitTarget"] != nil {
					return ErrInvalidResult
				}
			}
		}
		return nil
	}
	if !full.Result.Present {
		return nil
	}
	result := full.Result.Value
	if result.Status != "completed" && result.Status != "partial" && len(result.Findings) == 0 {
		return nil
	}
	task, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	want := contracts.GitDiffAssignment{SchemaVersion: 1, Execution: full.Execution, SourceSnapshotID: contracts.TaskID(fmt.Sprint(task.SourceSnapshotID)), SourceContentHash: contracts.Digest(task.SourceContentHash), Target: *target, EvidenceRevision: "head"}
	check := func(raw []byte) bool {
		v, e := contracts.DecodeGitDiffAssignment(raw, &full.Execution)
		return e == nil && sameJSON(v, want)
	}
	if !result.Extensions.Present || !check(result.Extensions.Value["gitDiff"]) {
		return ErrInvalidResult
	}
	for _, f := range result.Findings {
		if !f.Extensions.Present || !check(f.Extensions.Value["axiomGitTarget"]) {
			return ErrInvalidResult
		}
	}
	return nil
}
