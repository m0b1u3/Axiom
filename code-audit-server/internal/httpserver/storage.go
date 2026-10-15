package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	disk "axiom/code-audit-server/internal/storage"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"strconv"
	"strings"
	"syscall"
	"time"
)

func (s *Server) storageRoots() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range map[string]string{"scans": s.QueueLimits.OutputRoot, "snapshots": s.SnapshotDir} {
		if v != "" {
			p, e := disk.Root(v)
			if e != nil {
				return nil, e
			}
			out[k] = p
		}
	}
	if a, b := out["scans"], out["snapshots"]; a != "" && b != "" {
		if a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) || strings.HasPrefix(b, a+string(os.PathSeparator)) {
			return nil, disk.ErrBoundary
		}
	}
	return out, nil
}
func (s *Server) storageSnapshot(ctx context.Context, usage bool) (contracts.StorageSnapshot, error) {
	p, e := s.Store.StoragePolicy(ctx)
	if e != nil {
		return contracts.StorageSnapshot{}, e
	}
	stamp := contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
	out := contracts.StorageSnapshot{SchemaVersion: 1, Policy: p, State: "ready", Roots: []contracts.StorageRoot{}, ObservedAt: stamp, CleanupSupported: disk.CleanupSupported, AutomaticHistoryDeletion: false}
	roots, e := s.storageRoots()
	if e != nil || len(roots) == 0 {
		out.State = "unknown"
		return out, nil
	}
	space := s.StorageSpace
	if space == nil {
		space = disk.Space
	}
	for _, kind := range []string{"scans", "snapshots"} {
		root, ok := roots[kind]
		if !ok {
			continue
		}
		r := contracts.StorageRoot{Kind: contracts.StorageRootKind(kind), State: "ready", ObservedAt: stamp}
		free, total, e := space(root)
		if e != nil || free < 0 || total <= 0 || free > disk.MaxBytes || total > disk.MaxBytes || free > total {
			r.State = "unknown"
			r.Message = "无法获取可信的卷容量，已暂停新领取"
		} else {
			r.AvailableBytes = &free
			r.VolumeBytes = &total
			if free < p.MinFreeBytes {
				r.State = "low"
				r.Message = "剩余空间低于领取阈值，等待释放空间或调整策略"
			}
		}
		if usage {
			used, _, err := disk.Usage(ctx, root, ".", false)
			if err == nil {
				r.UsedBytes = &used
			} else {
				r.Message += "；目录占用检查未完成，不能作为零占用"
			}
		}
		if r.State == "unknown" {
			out.State = "unknown"
		} else if r.State == "low" && out.State != "unknown" {
			out.State = "low"
		}
		out.Roots = append(out.Roots, r)
	}
	return out, nil
}
func (s *Server) storageClaimLimits(ctx context.Context) store.ClaimLimits {
	limits := s.QueueLimits
	// Old metadata-only readers have no managed execution root. Production always
	// supplies one; configured roots fail closed when probes are unavailable.
	if limits.OutputRoot == "" {
		return limits
	}
	p, e := s.Store.StoragePolicy(ctx)
	roots, rootErr := s.storageRoots()
	space := s.StorageSpace
	if space == nil {
		space = disk.Space
	}
	limits.StorageCheck = func(context.Context) string {
		if e != nil || rootErr != nil || len(roots) == 0 {
			return "storage_unknown"
		}
		for _, root := range roots {
			free, total, e := space(root)
			if e != nil || free < 0 || total <= 0 || total < free || total > disk.MaxBytes {
				return "storage_unknown"
			}
			if free < p.MinFreeBytes {
				return "storage_low"
			}
		}
		return ""
	}
	return limits
}
func (s *Server) readStorage(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	v, e := s.storageSnapshot(ctx, true)
	if e != nil {
		s.storageError(w, e)
		return
	}
	storageOK(w, contracts.KindStorageSnapshot, v)
}
func (s *Server) updateStorage(w http.ResponseWriter, r *http.Request, u *store.User) {
	var p contracts.StoragePolicy
	if !smallJSON(w, r, &p) {
		return
	}
	if e := s.Store.UpdateStoragePolicy(r.Context(), p); e != nil {
		s.storageError(w, e)
		return
	}
	s.readStorage(w, r, u)
}
func storageOK(w http.ResponseWriter, kind contracts.Kind, v any) {
	raw, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(kind, raw, nil) != nil {
		httputil.Error(w, 503, 50300, "存储状态未通过合同校验，请重试")
		return
	}
	httputil.OK(w, v)
}
func (s *Server) storageError(w http.ResponseWriter, e error) {
	log.Printf("storage maintenance: %v", e)
	code, message := 503, "存储维护暂不可用；已有数据与清理记录保留，请刷新后继续"
	if errors.Is(e, sql.ErrNoRows) {
		code, message = 404, "任务、快照或清理记录不存在"
	}
	if errors.Is(e, store.ErrStorageProtected) || errors.Is(e, store.ErrStorageConflict) || errors.Is(e, disk.ErrBoundary) || errors.Is(e, disk.ErrChanged) {
		code, message = 409, e.Error()
	}
	httputil.Error(w, code, code*100, message)
}
func (s *Server) listCleanup(w http.ResponseWriter, r *http.Request, u *store.User) {
	v, e := s.Store.CleanupJobs(r.Context(), false)
	if e != nil {
		s.storageError(w, e)
		return
	}
	httputil.OK(w, v)
}
func (s *Server) readCleanup(w http.ResponseWriter, r *http.Request, u *store.User) {
	v, _, e := s.Store.CleanupJob(r.Context(), r.PathValue("job"))
	if e != nil {
		s.storageError(w, e)
		return
	}
	storageOK(w, contracts.KindCleanupJob, v)
}
func (s *Server) previewCleanup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Kind       string `json:"kind"`
		ResourceID int64  `json:"resourceId"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	v, e := s.makeCleanup(r.Context(), body.Kind, body.ResourceID, u.Username)
	if e != nil {
		s.storageError(w, e)
		return
	}
	storageOK(w, contracts.KindCleanupJob, v)
}

func (s *Server) makeCleanup(ctx context.Context, kind string, id int64, actor string) (contracts.CleanupJob, error) {
	var job contracts.CleanupJob
	if !disk.CleanupSupported {
		return job, disk.ErrBoundary
	}
	if kind != "orphans" && kind != "scan" && kind != "snapshot" {
		return job, store.ErrStorageConflict
	}
	if kind != "orphans" && id < 1 {
		return job, store.ErrStorageConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	e := s.Store.StorageLock(ctx, false, func() error {
		roots, e := s.storageRoots()
		if e != nil {
			return e
		}
		policy, e := s.Store.StoragePolicy(ctx)
		if e != nil {
			return e
		}
		refs, e := s.Store.StorageReferences(ctx)
		if e != nil {
			return e
		}
		active, e := s.Store.CleanupJobs(ctx, true)
		if e != nil {
			return e
		}
		targets := map[string]map[string]bool{}
		if kind != "orphans" {
			paths, e := s.Store.CleanupResourcePaths(ctx, kind, id, "")
			if e != nil {
				return e
			}
			key := "scans"
			if kind == "snapshot" {
				key = "snapshots"
			}
			targets[key] = map[string]bool{}
			for _, path := range paths {
				if path == "" {
					continue
				}
				name, e := disk.Top(roots[key], path)
				if e != nil {
					return e
				}
				targets[key][name] = true
			}
		}
		random := make([]byte, 16)
		if _, e = rand.Read(random); e != nil {
			return e
		}
		now := time.Now().UTC()
		job = contracts.CleanupJob{SchemaVersion: 1, ID: contracts.Identifier(hex.EncodeToString(random)), Kind: contracts.CleanupJobKind(kind), State: "preview", Revision: 1, CreatedAt: contracts.Timestamp(now.Format(time.RFC3339Nano)), UpdatedAt: contracts.Timestamp(now.Format(time.RFC3339Nano)), ExpiresAt: contracts.Timestamp(now.Add(15 * time.Minute).Format(time.RFC3339Nano)), CreatedBy: actor, Items: []contracts.CleanupItem{}}
		if kind != "orphans" {
			v := contracts.TaskID(strconv.FormatInt(id, 10))
			job.ResourceID = &v
		}
		protectedCount := 0
		for _, key := range []string{"scans", "snapshots"} {
			root, ok := roots[key]
			if !ok {
				if len(targets[key]) > 0 {
					return disk.ErrBoundary
				}
				continue
			}
			entries, e := disk.Entries(root)
			if e != nil {
				return e
			}
			found := map[string]bool{}
			for _, entry := range entries {
				name := entry.Name()
				if name == disk.Trash {
					continue
				}
				if kind != "orphans" && !targets[key][name] {
					continue
				}
				found[name] = true
				if len(job.Items) >= 200 {
					job.Truncated = true
					break
				}
				item := contracts.CleanupItem{Root: contracts.CleanupItemRoot(key), RelativePath: contracts.RelativePath(name), State: "planned", Reason: "unreferenced"}
				for _, ref := range refs {
					if ref.Path == "" {
						continue
					}
					if kind != "orphans" && ref.Kind == kind && ref.ID == id {
						continue
					}
					if related, e := disk.Related(root, name, ref.Path); e == nil && related {
						item.State = "protected"
						item.Reason = ref.Reason
						break
					}
				}
				for _, j := range active {
					for _, v := range j.Items {
						if v.Root == item.Root && v.RelativePath == item.RelativePath && v.State != "protected" && v.State != "purged" {
							item.State = "protected"
							item.Reason = "cleanup_in_progress"
						}
					}
				}
				info, e := entry.Info()
				if e != nil {
					return e
				}
				if entry.Type()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
					item.State = "protected"
					item.Reason = "unsafe_entry"
				}
				if kind == "orphans" && item.State == "planned" && now.Sub(info.ModTime()) < time.Duration(policy.OrphanGraceHours)*time.Hour {
					item.State = "protected"
					item.Reason = "grace_period"
				}
				if kind == "orphans" && key == "scans" && item.State == "planned" {
					evidence, err := disk.HasEvidence(ctx, root, name)
					if err != nil {
						item.State = "protected"
						item.Reason = "inspection_incomplete"
					} else if evidence {
						item.State = "protected"
						item.Reason = "unregistered_evidence"
					}
				}
				if kind == "orphans" && item.State == "protected" {
					if protectedCount >= 40 {
						job.Truncated = true
						continue
					}
					protectedCount++
				}
				size, hash, e := disk.Usage(ctx, root, name, item.State == "planned")
				if e != nil {
					item.State = "protected"
					item.Reason = "inspection_incomplete"
				} else {
					item.Bytes = &size
					if item.State == "planned" {
						item.Fingerprint = hash
					}
				}
				if kind != "orphans" && item.State == "protected" {
					return store.ErrStorageProtected
				}
				job.Items = append(job.Items, item)
			}
			if kind != "orphans" {
				for name := range targets[key] {
					if !found[name] {
						job.Items = append(job.Items, contracts.CleanupItem{Root: contracts.CleanupItemRoot(key), RelativePath: contracts.RelativePath(name), State: "purged", Reason: "already_absent"})
					}
				}
			}
		}
		if kind != "orphans" && job.Truncated {
			return store.ErrStorageConflict
		}
		encoded, _ := json.Marshal(roots)
		return s.Store.SaveCleanupJob(ctx, job, string(encoded), true)
	})
	return job, e
}
func (s *Server) executeCleanup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Revision int64 `json:"revision"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	v, e := s.runCleanup(r.Context(), r.PathValue("job"), body.Revision)
	if e != nil {
		s.storageError(w, e)
		return
	}
	storageOK(w, contracts.KindCleanupJob, v)
}

func (s *Server) runCleanup(ctx context.Context, id string, revision int64) (contracts.CleanupJob, error) {
	var job contracts.CleanupJob
	if !disk.CleanupSupported {
		return job, disk.ErrBoundary
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	e := s.Store.StorageLock(ctx, true, func() error {
		var saved string
		var e error
		job, saved, e = s.Store.CleanupJob(ctx, id)
		if e != nil {
			return e
		}
		if job.State == "completed" {
			return nil
		}
		if job.State == "canceled" {
			return store.ErrStorageConflict
		}
		if job.Revision != revision {
			return store.ErrStorageConflict
		}
		roots, e := s.storageRoots()
		if e != nil {
			return e
		}
		encoded, _ := json.Marshal(roots)
		if string(encoded) != saved {
			return disk.ErrBoundary
		}
		if job.State == "preview" {
			expires, e := time.Parse(time.RFC3339Nano, string(job.ExpiresAt))
			if e != nil || time.Now().After(expires) {
				return store.ErrStorageConflict
			}
		}
		resourceID := int64(0)
		if job.ResourceID != nil {
			resourceID, _ = strconv.ParseInt(string(*job.ResourceID), 10, 64)
			if _, e = s.Store.CleanupResourcePaths(ctx, string(job.Kind), resourceID, id); e != nil {
				return e
			}
		}
		refs, e := s.Store.StorageReferences(ctx)
		if e != nil {
			return e
		}
		// A crash can occur after rename but before the phase update. Discover the
		// owned quarantine before checking newly-created references at the old path.
		for index := range job.Items {
			item := &job.Items[index]
			if item.State != "planned" {
				continue
			}
			moved, err := disk.Quarantined(roots[string(item.Root)], fmt.Sprintf("%s-%d", job.ID, index))
			if err != nil {
				return err
			}
			if moved {
				item.State = "quarantined"
			}
		}
		policy, e := s.Store.StoragePolicy(ctx)
		if e != nil {
			return e
		}
		active, e := s.Store.CleanupJobs(ctx, true)
		if e != nil {
			return e
		}
		for _, item := range job.Items {
			if item.State != "planned" {
				continue
			}
			for _, other := range active {
				if other.ID == job.ID {
					continue
				}
				for _, v := range other.Items {
					if v.State != "protected" && v.State != "purged" && v.Root == item.Root && v.RelativePath == item.RelativePath {
						return store.ErrStorageProtected
					}
				}
			}
			if job.Kind == "orphans" {
				info, e := os.Lstat(filepath.Join(roots[string(item.Root)], string(item.RelativePath)))
				if e == nil && time.Since(info.ModTime()) < time.Duration(policy.OrphanGraceHours)*time.Hour {
					return disk.ErrChanged
				}
				if e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
		// Recheck references immediately before the first rename, under the same
		// cross-process gate used by admission, retries and source uploads.
		for _, item := range job.Items {
			if item.State != "planned" {
				continue
			}
			root := roots[string(item.Root)]
			for _, ref := range refs {
				if job.ResourceID != nil && ref.Kind == string(job.Kind) && ref.ID == resourceID {
					continue
				}
				if ref.Path != "" {
					if related, e := disk.Related(root, string(item.RelativePath), ref.Path); e == nil && related {
						return store.ErrStorageProtected
					}
				}
			}
		}
		job.State = "running"
		job.Error = ""
		job.Revision++
		job.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
		if e = s.Store.SaveCleanupJob(ctx, job, saved, false); e != nil {
			return e
		}
		failed := func(cause error) error {
			job.State = "failed"
			job.Error = "清理未完成；保留记录，可从 Web 继续。"
			if errors.Is(cause, fs.ErrPermission) {
				job.Error = "隔离目录权限不足，数据保留；修复目录或挂载权限后可继续。"
			} else if errors.Is(cause, syscall.ENOSPC) {
				job.Error = "存储写入空间不足；释放空间后可继续处理本记录。"
			} else if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
				job.Error = "本轮清理请求中断或超时；已记录进度，可继续处理剩余对象。"
			}
			if errors.Is(cause, disk.ErrChanged) || errors.Is(cause, disk.ErrBoundary) {
				job.Error = cause.Error()
			}
			job.Revision++
			job.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
			save, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.Store.SaveCleanupJob(save, job, saved, false); err != nil {
				return err
			}
			return cause
		}
		for index := range job.Items {
			item := &job.Items[index]
			if item.State == "protected" || item.State == "purged" {
				continue
			}
			root := roots[string(item.Root)]
			destination := fmt.Sprintf("%s-%d", job.ID, index)
			if item.State == "planned" {
				moved, e := disk.Quarantine(ctx, root, string(item.RelativePath), destination, item.Fingerprint)
				if e != nil {
					return failed(e)
				}
				if s.StorageCheckpoint != nil {
					if e = s.StorageCheckpoint("quarantined"); e != nil {
						return failed(e)
					}
				}
				if moved {
					item.State = "quarantined"
				} else {
					item.State = "purged"
				}
				job.Revision++
				job.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
				if e = s.Store.SaveCleanupJob(ctx, job, saved, false); e != nil {
					return failed(e)
				}
			}
			if item.State == "quarantined" {
				purge := s.StoragePurge
				if purge == nil {
					purge = disk.Purge
				}
				if e = purge(ctx, root, destination); e != nil {
					return failed(e)
				}
				item.State = "purged"
				job.Revision++
				job.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
				if e = s.Store.SaveCleanupJob(ctx, job, saved, false); e != nil {
					return failed(e)
				}
			}
		}
		if e = s.Store.FinishCleanup(ctx, job); e != nil {
			return failed(e)
		}
		job, _, e = s.Store.CleanupJob(ctx, id)
		return e
	})
	return job, e
}

func (s *Server) deleteManaged(w http.ResponseWriter, r *http.Request, u *store.User, kind string) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "对象 ID 无效")
		return
	}
	j, e := s.makeCleanup(r.Context(), kind, id, u.Username)
	if e == nil {
		j, e = s.runCleanup(r.Context(), string(j.ID), j.Revision)
	}
	if e != nil {
		s.storageError(w, e)
		return
	}
	httputil.OK(w, map[string]any{"id": id, "cleanupJobId": j.ID})
}

func (s *Server) cancelCleanup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Revision int64 `json:"revision"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	var j contracts.CleanupJob
	e := s.Store.StorageLock(r.Context(), true, func() error {
		var rootsJSON string
		var e error
		j, rootsJSON, e = s.Store.CleanupJob(r.Context(), r.PathValue("job"))
		if e != nil {
			return e
		}
		if j.Revision != body.Revision || j.State == "completed" || j.State == "canceled" {
			return store.ErrStorageConflict
		}
		roots, e := s.storageRoots()
		if e != nil {
			return e
		}
		encoded, _ := json.Marshal(roots)
		if string(encoded) != rootsJSON {
			return disk.ErrBoundary
		}
		for index, item := range j.Items {
			if item.State == "quarantined" {
				return store.ErrStorageProtected
			}
			if item.State == "planned" {
				moved, e := disk.Quarantined(roots[string(item.Root)], fmt.Sprintf("%s-%d", j.ID, index))
				if e != nil {
					return e
				}
				if moved {
					return store.ErrStorageProtected
				}
			}
		}
		j.State = "canceled"
		j.Error = "本次清理已放弃；尚未清理的数据保留。"
		j.Revision++
		j.UpdatedAt = contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))
		return s.Store.SaveCleanupJob(r.Context(), j, rootsJSON, false)
	})
	if e != nil {
		s.storageError(w, e)
		return
	}
	storageOK(w, contracts.KindCleanupJob, j)
}
