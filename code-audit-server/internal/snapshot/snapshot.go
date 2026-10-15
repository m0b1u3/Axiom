package snapshot

import (
	"archive/tar"
	"archive/zip"
	"axiom/sourceidentity"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Limits 对应开发计划第 23 节的默认限制。
type Limits struct {
	MaxArchiveBytes     int64 // 上传归档上限
	MaxExtractedBytes   int64 // 解压后总字节上限
	MaxFiles            int   // 文件数上限
	MaxFileBytes        int64 // 单文件上限
	MaxCompressionRatio int64 // 累计压缩比上限（仅 zip，防炸弹）
}

func DefaultLimits() Limits {
	return Limits{
		MaxArchiveBytes:     2 << 30,
		MaxExtractedBytes:   10 << 30,
		MaxFiles:            200_000,
		MaxFileBytes:        100 << 20,
		MaxCompressionRatio: 1000,
	}
}

type Result struct {
	ArchiveSha256 string
	FileCount     int
	TotalSize     int64
	ContentHash   string
	SnapshotPath  string
}

func IsSupportedArchive(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz")
}

// HashArchive 把上传流写入临时文件并计算 SHA256，同时校验归档大小。
// 调用方负责删除返回的临时文件。
func HashArchive(input io.Reader, limits Limits) (tmpPath, sha string, size int64, err error) {
	return HashArchiveIn(input, limits, "")
}

// HashArchiveIn keeps production upload remnants in a managed private directory
// so an interrupted upload is visible to the same preview/cleanup workflow.
func HashArchiveIn(input io.Reader, limits Limits, directory string) (tmpPath, sha string, size int64, err error) {
	if directory != "" {
		if err = os.MkdirAll(directory, 0700); err != nil {
			return
		}
		var info os.FileInfo
		info, err = os.Lstat(directory)
		if err != nil {
			return
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			err = fmt.Errorf("upload temporary directory is not a private directory")
			return
		}
	}
	f, err := os.CreateTemp(directory, "axiom-upload-*")
	if err != nil {
		return "", "", 0, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	limited := io.LimitReader(input, limits.MaxArchiveBytes+1)
	n, err := io.Copy(io.MultiWriter(f, h), limited)
	if err != nil {
		return "", "", 0, err
	}
	if n > limits.MaxArchiveBytes {
		return "", "", 0, fmt.Errorf("archive exceeds %d bytes limit", limits.MaxArchiveBytes)
	}
	if err := f.Close(); err != nil {
		return "", "", 0, err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), n, nil
}

// ExtractArchive 只接受 .zip / .tar.gz / .tgz。
// 任何条目违规（逃逸 / 符号链接 / 超限）→ 直接失败并删除半成品。
// 完成后目录即不可变快照，禁止再次写入。
func ExtractArchive(tmpPath, originalFilename, destDir string, limits Limits) (*Result, error) {
	if !IsSupportedArchive(originalFilename) {
		return nil, fmt.Errorf("unsupported archive type: %s (only .zip / .tar.gz / .tgz)", originalFilename)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return nil, err
	}
	state := &extractState{limits: limits}

	var err error
	switch {
	case strings.HasSuffix(strings.ToLower(originalFilename), ".zip"):
		err = extractZip(tmpPath, destDir, state)
	default:
		err = extractTarGz(tmpPath, destDir, state)
	}
	if err != nil {
		_ = os.RemoveAll(destDir)
		return nil, err
	}
	contentHash, count, size, err := treeHash(destDir)
	if err != nil {
		_ = os.RemoveAll(destDir)
		return nil, err
	}
	return &Result{
		FileCount:    count,
		TotalSize:    size,
		ContentHash:  contentHash,
		SnapshotPath: destDir,
	}, nil
}

type extractState struct {
	limits         Limits
	files          int
	totalSize      int64
	compressedSize int64
}

// sanitize 返回规范化后的相对路径（正斜杠），空串表示目录可跳过。
func sanitize(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" {
		return "", fmt.Errorf("empty entry name")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute path not allowed: %s", name)
	}
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("NUL byte in entry name")
	}
	lower := name
	if len(lower) >= 2 && lower[1] == ':' && isLetter(lower[0]) {
		return "", fmt.Errorf("drive path not allowed: %s", name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes destination: %s", name)
	}
	if strings.Contains(clean, ":") {
		return "", fmt.Errorf("reserved colon in path not allowed: %s", name)
	}
	// Windows 兼容限制：单段 ≤ 255 字节、嵌套 ≤ 48 层（mkdir/OpenFile 对超限报
	// "The system cannot find the path specified"（ERROR_PATH_NOT_FOUND））
	segs := strings.Split(clean, "/")
	if len(segs) > 48 {
		return "", fmt.Errorf("path too deep (>48 segments): %s", name)
	}
	for _, seg := range segs {
		if len(seg) > 255 {
			return "", fmt.Errorf("path segment too long (>255 bytes): %s", seg[:40]+"...")
		}
	}
	return clean, nil
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func (s *extractState) entry(fileSize, compressedSize int64) error {
	s.files++
	if s.files > s.limits.MaxFiles {
		return fmt.Errorf("more than %d files in archive", s.limits.MaxFiles)
	}
	if fileSize > s.limits.MaxFileBytes {
		return fmt.Errorf("single file exceeds %d bytes", s.limits.MaxFileBytes)
	}
	s.totalSize += fileSize
	if s.totalSize > s.limits.MaxExtractedBytes {
		return fmt.Errorf("extracted content exceeds %d bytes", s.limits.MaxExtractedBytes)
	}
	if compressedSize > 0 && fileSize > 0 {
		s.compressedSize += compressedSize
		ratio := s.totalSize / (s.compressedSize + 1)
		if ratio > s.limits.MaxCompressionRatio {
			return fmt.Errorf("unusual compression ratio %d exceeds limit", ratio)
		}
	}
	return nil
}

// longPath：Windows 超长路径加 `\\?\` 前缀（组件级限制已在 sanitize 收紧，
// 这里兜底总路径 > 240 的情况）。
func longPath(p string) string {
	if runtime.GOOS != "windows" || !filepath.IsAbs(p) || strings.HasPrefix(p, `\\?\`) {
		return p
	}
	return `\\?\` + p
}

// ensureDirFor：逐段创建父目录，并检测「目录位置被同名文件占据」的碰撞
// （zip 中同名文件条目先出现时会触发 ERROR_PATH_NOT_FOUND）。
func ensureDirFor(dest, rel string, writtenFiles map[string]bool) error {
	dirs := filepath.Dir(filepath.FromSlash(rel))
	if dirs == "." || dirs == "" {
		return nil
	}
	cur := dest
	for _, seg := range strings.Split(filepath.FromSlash(dirs), string(os.PathSeparator)) {
		if seg == "" {
			continue
		}
		cur = filepath.Join(cur, seg)
		if writtenFiles[cur] {
			return fmt.Errorf("path %s conflicts with an existing file entry", filepath.Join(rel))
		}
		info, err := os.Stat(cur)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err := os.Mkdir(cur, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", cur, err)
			}
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("mkdir %s: path exists and is not a directory", cur)
		}
	}
	return nil
}

func writeEntry(dest string, rel string, r io.Reader, size int64, budget *int64, seenFiles map[string]bool, seenDirs map[string]bool) error {
	if size > *budget {
		return fmt.Errorf("extracted content exceeds budget")
	}
	full := filepath.Join(dest, filepath.FromSlash(rel))
	// 二次确认：清理后仍须位于 dest 之内（防含 `a/../..` 的组合）。
	relBack, err := filepath.Rel(dest, filepath.Clean(full))
	if err != nil || strings.Contains(relBack, "..") || filepath.IsAbs(relBack) {
		return fmt.Errorf("resolved path escapes destination: %s", rel)
	}
	if len(full) > 240 {
		full = longPath(full)
	}
	if err := ensureDirFor(dest, rel, seenFiles); err != nil {
		return err
	}
	// 写入目标本身已是目录（来自显式目录条目或前序目录）→ 碰撞
	if seenDirs[filepath.Join(dest, filepath.FromSlash(rel))] {
		return fmt.Errorf("write %s: path is a directory", rel)
	}
	if info, err := os.Stat(full); err == nil && info.IsDir() {
		return fmt.Errorf("write %s: path is a directory", rel)
	}
	out, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	defer out.Close()
	written, err := io.CopyN(out, r, size)
	if err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	*budget -= written
	seenFiles[filepath.Join(dest, filepath.FromSlash(rel))] = true
	seenFiles[filepath.Clean(full)] = true
	return nil
}

func extractZip(tmpPath, destDir string, state *extractState) error {
	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	budget := state.limits.MaxExtractedBytes
	seenFiles := map[string]bool{}
	seenDirs := map[string]bool{}
	for _, fh := range zr.File {
		if fh.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink entry not allowed: %s", fh.Name)
		}
		if fh.Mode()&os.ModeIrregular != 0 && fh.Mode()&os.ModeDir == 0 {
			return fmt.Errorf("irregular file entry not allowed: %s", fh.Name)
		}
		// 目录条目（尾斜杠 / ModeDir）：显式建目录并登记，禁止视为文件
		if strings.HasSuffix(fh.Name, "/") || fh.Mode()&os.ModeDir != 0 {
			rel, err := sanitize(strings.TrimSuffix(fh.Name, "/"))
			if err != nil {
				return err
			}
			if rel != "" {
				if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(rel)), 0o755); err != nil {
					return fmt.Errorf("mkdir %s: %w", rel, err)
				}
				seenDirs[filepath.Join(destDir, filepath.FromSlash(rel))] = true
			}
			continue
		}
		rel, err := sanitize(fh.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		if err := state.entry(int64(fh.UncompressedSize64), int64(fh.CompressedSize64)); err != nil {
			return err
		}
		if fh.UncompressedSize64 > uint64(state.limits.MaxFileBytes) {
			return fmt.Errorf("single file exceeds limit: %s", fh.Name)
		}
		rc, err := fh.Open()
		if err != nil {
			return fmt.Errorf("open entry %s: %w", fh.Name, err)
		}
		err = writeEntry(destDir, rel, rc, int64(fh.UncompressedSize64), &budget, seenFiles, seenDirs)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(tmpPath, destDir string, state *extractState) error {
	f, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	budget := state.limits.MaxExtractedBytes
	seenFiles := map[string]bool{}
	seenDirs := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			rel, err := sanitize(hdr.Name)
			if err != nil {
				return err
			}
			if rel != "" {
				if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(rel)), 0o755); err != nil {
					return err
				}
				seenDirs[filepath.Join(destDir, filepath.FromSlash(rel))] = true
			}
			continue
		case tar.TypeReg:
			// 正常文件
		default:
			return fmt.Errorf("special file entry not allowed (type %d): %s", hdr.Typeflag, hdr.Name)
		}
		rel, err := sanitize(hdr.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		if hdr.Size < 0 {
			return fmt.Errorf("negative size entry: %s", hdr.Name)
		}
		if err := state.entry(hdr.Size, 0); err != nil {
			return err
		}
		if err := writeEntry(destDir, rel, tr, hdr.Size, &budget, seenFiles, seenDirs); err != nil {
			return err
		}
	}
	return nil
}

// treeHash 生成内容哈希：文件名 + 大小 + 文件 SHA256，升序拼接后再哈希。
func treeHash(root string) (string, int, int64, error) {
	return sourceidentity.TreeHash(context.Background(), root)
}
