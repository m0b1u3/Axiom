package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) uploadGitSnapshot(w http.ResponseWriter, r *http.Request, u *store.User) {
	if e := s.Store.StorageLock(r.Context(), false, func() error { s.uploadGitSnapshotData(w, r); return nil }); e != nil {
		s.storageError(w, e)
	}
}
func (s *Server) uploadGitSnapshotData(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	limits := s.Limits
	if limits.MaxArchiveBytes == 0 {
		limits = snapshot.DefaultLimits()
	}
	r.Body = http.MaxBytesReader(w, r.Body, limits.MaxArchiveBytes+(1<<20))
	if e := r.ParseMultipartForm(8 << 20); e != nil {
		httputil.Error(w, 400, 40000, "Git 归档上传不完整或超过容量限制")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	input, header, e := r.FormFile("file")
	if e != nil {
		httputil.Error(w, 400, 40000, "请选择包含 .git 历史的仓库归档")
		return
	}
	defer input.Close()
	if !snapshot.IsSupportedArchive(header.Filename) {
		httputil.Error(w, 400, 40000, "Git 仓库归档仅支持 .zip / .tar.gz / .tgz")
		return
	}
	base, head := strings.TrimSpace(r.FormValue("base")), strings.TrimSpace(r.FormValue("head"))
	if base == "" || head == "" {
		httputil.Error(w, 400, 40000, "请明确选择 base 和 head 提交")
		return
	}
	var project int64
	if value := r.FormValue("projectId"); value != "" {
		project, e = strconv.ParseInt(value, 10, 64)
		if e != nil || project < 1 {
			httputil.Error(w, 400, 40000, "项目编号无效")
			return
		}
		if _, e = s.Store.GetProject(ctx, project); e != nil {
			httputil.Error(w, 400, 40000, "项目不存在")
			return
		}
	}
	staging := filepath.Join(s.SnapshotDir, ".uploads")
	if e = os.MkdirAll(staging, 0700); e != nil {
		httputil.Error(w, 503, 50300, "快照暂存目录不可用")
		return
	}
	archive, uploaded, _, e := snapshot.HashArchiveIn(input, limits, staging)
	if e != nil {
		httputil.Error(w, 400, 40000, "Git 归档超过上传限制")
		return
	}
	defer os.Remove(archive)
	workspace, e := os.MkdirTemp(staging, "git-freeze-")
	if e != nil {
		httputil.Error(w, 503, 50300, "无法创建 Git 冻结目录")
		return
	}
	defer os.RemoveAll(workspace)
	result, target, e := snapshot.ImportGitArchive(ctx, archive, header.Filename, filepath.Join(workspace, "snapshot"), base, head, limits)
	if e != nil {
		httputil.Error(w, 400, 40000, "Git 快照未创建："+e.Error())
		return
	}
	if existing, e := s.Store.GetSnapshotByArchiveSha256(ctx, result.ArchiveSha256); e == nil {
		saved, err := s.Store.GitSnapshotTarget(ctx, existing.ID)
		if err != nil || saved == nil || *saved != *target {
			httputil.Error(w, 409, 40900, "已有快照未记录一致的 Git 身份，未覆盖原快照")
			return
		}
		httputil.OK(w, map[string]any{"snapshot": existing, "target": saved})
		return
	}
	final := filepath.Join(s.SnapshotDir, "git-"+result.ArchiveSha256)
	if e = os.Rename(result.SnapshotPath, final); e != nil {
		httputil.Error(w, 409, 40900, "Git 冻结目录已存在或不可写；未覆盖已有数据，请重新读取资产后重试")
		return
	}
	row, e := s.Store.RegisterGitSnapshot(ctx, store.SnapshotRow{OriginalFilename: "[Git diff] " + header.Filename, ArchiveSha256: result.ArchiveSha256, ContentHash: result.ContentHash, SnapshotPath: final, FileCount: result.FileCount, TotalSize: result.TotalSize, ProjectID: project}, *target, uploaded)
	if e != nil {
		httputil.Error(w, 503, 50300, "Git 冻结文件已保留，但元数据暂时无法保存；可从存储预览定位未登记目录")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, map[string]any{"snapshot": row, "target": target})
}

func (s *Server) frozenGitDiff(ctx context.Context, id int64) (*store.SnapshotRow, *contracts.GitDiffTarget, []contracts.GitDiffFile, error) {
	snap, e := s.Store.GetSnapshot(ctx, id)
	if e != nil {
		return nil, nil, nil, e
	}
	target, e := s.Store.GitSnapshotTarget(ctx, id)
	if e != nil {
		return nil, nil, nil, e
	}
	if target == nil {
		return nil, nil, nil, errors.New("快照没有冻结的 Git 提交差异")
	}
	hash, _, _, e := sourceidentity.TreeHash(ctx, snap.SnapshotPath)
	if e != nil || hash != snap.ContentHash {
		return nil, nil, nil, errors.New("Git 快照完整性不匹配或已不可用；未改用其他源码")
	}
	if e = sourceidentity.VerifyGitDiff(ctx, snap.SnapshotPath, target.BaseCommit, target.HeadCommit, target.BaseTree, target.HeadTree, string(target.DiffSHA256), string(target.HeadContentHash)); e != nil {
		return nil, nil, nil, e
	}
	files, e := snapshot.GitDiffFiles(ctx, snap.SnapshotPath, *target)
	return snap, target, files, e
}
func (s *Server) gitDiffPreview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	page := 1
	var e error
	if value := r.URL.Query().Get("page"); value != "" {
		page, e = strconv.Atoi(value)
	}
	if !ok || e != nil || page < 1 || page > 9007199254740991 {
		httputil.Error(w, 400, 40000, "快照编号或差异页码无效")
		return
	}
	if e = s.Store.StorageLock(r.Context(), false, func() error {
		// Distinguish an ordinary historical snapshot from an unavailable Git one.
		target, err := s.Store.GitSnapshotTarget(r.Context(), id)
		if err != nil {
			return err
		}
		if target == nil {
			httputil.Error(w, 404, 40400, "此源码快照没有 Git 提交差异，仍可按原方式扫描")
			return nil
		}
		snap, target, files, err := s.frozenGitDiff(r.Context(), id)
		if err != nil {
			return err
		}
		// Clamp before multiplying so even the largest accepted integer cannot overflow.
		start := len(files)
		if page-1 <= len(files)/50 {
			start = min((page-1)*50, len(files))
		}
		end := min(start+50, len(files))
		v := contracts.GitDiffPreview{SchemaVersion: 1, SnapshotID: contracts.TaskID(fmt.Sprint(id)), ContentHash: contracts.Digest(snap.ContentHash), Target: *target, Files: files[start:end], Page: int64(page), PageSize: 50, Total: int64(len(files))}
		raw, err := contracts.EncodeJSON(v)
		if err != nil {
			return err
		}
		if _, err = contracts.DecodeGitDiffPreview(raw, nil); err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		httputil.OK(w, v)
		return nil
	}); e != nil {
		httputil.Error(w, 409, 40900, "Git 差异暂不可读取："+e.Error())
	}
}
func (s *Server) gitDiffContent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	index, e := strconv.Atoi(r.PathValue("file"))
	if !ok || e != nil || index < 0 {
		httputil.Error(w, 400, 40000, "快照或差异文件编号无效")
		return
	}
	if e = s.Store.StorageLock(r.Context(), false, func() error {
		snap, target, files, err := s.frozenGitDiff(r.Context(), id)
		if err != nil {
			return err
		}
		if index >= len(files) {
			httputil.Error(w, 404, 40400, "差异文件不存在")
			return nil
		}
		file := files[index]
		args := sourceidentity.GitDiffArgs(target.BaseCommit, target.HeadCommit)
		if file.OldPath != nil {
			args = append(args, string(*file.OldPath))
		}
		if file.NewPath != nil && (file.OldPath == nil || *file.NewPath != *file.OldPath) {
			args = append(args, string(*file.NewPath))
		}
		patch, err := sourceidentity.Git(r.Context(), snap.SnapshotPath, 64<<20, args...)
		if err != nil {
			return err
		}
		v := contracts.GitDiffContent{SchemaVersion: 1, SnapshotID: contracts.TaskID(fmt.Sprint(id)), ContentHash: contracts.Digest(snap.ContentHash), Target: *target, File: file, Patch: string(patch)}
		if len(patch) > 262144 {
			patch = patch[:262144]
			for !utf8.Valid(patch) && len(patch) > 0 {
				patch = patch[:len(patch)-1]
			}
			v.Patch = string(patch)
			v.Truncated = true
			reason := "差异超过 256 KiB，仅预览前段；扫描目标与冻结摘要仍覆盖完整差异。"
			v.Reason = &reason
		}
		if strings.Contains(v.Patch, "\nBinary files ") {
			reason := "二进制文件仅展示 Git 差异元信息。"
			v.Reason = &reason
		}
		if !utf8.ValidString(v.Patch) {
			v.Patch = strings.ToValidUTF8(v.Patch, "�")
			reason := "差异包含非 UTF-8 字节，预览使用替代字符；冻结摘要仍基于原始字节。"
			v.Reason = &reason
		}
		raw, err := contracts.EncodeJSON(v)
		if err != nil {
			return err
		}
		if _, err = contracts.DecodeGitDiffContent(raw, nil); err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		httputil.OK(w, v)
		return nil
	}); e != nil {
		httputil.Error(w, 409, 40900, "Git 文件差异暂不可读取："+e.Error())
	}
}
