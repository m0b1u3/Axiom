package snapshot_test

import (
	"archive/tar"
	"path"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"axiom/code-audit-server/internal/snapshot"
)

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func limits() snapshot.Limits {
	l := snapshot.DefaultLimits()
	l.MaxArchiveBytes = 10 << 20
	l.MaxExtractedBytes = 10 << 20
	l.MaxFileBytes = 1 << 20
	return l
}

func writeZip(t *testing.T, entries []map[string]any) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "upload.zip")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range entries {
		name := e["name"].(string)
		var hdr *zip.FileHeader
		if mode, ok := e["mode"].(uint32); ok && mode != 0 {
			hdr = &zip.FileHeader{Name: name, Method: zip.Deflate}
			hdr.SetMode(os.FileMode(mode))
		} else {
			hdr = &zip.FileHeader{Name: name, Method: zip.Deflate}
		}
		dw, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dw.Write([]byte(e["data"].(string))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestSafeZipExtractsOnlyCleanEntries(t *testing.T) {
	zipPath := writeZip(t, []map[string]any{
		{"name": "src/app.py", "data": "print(1)\n"},
		{"name": "src/readme.md", "data": "docs\n"},
		{"name": "../evil.txt", "data": "pwned"},
		{"name": "/abs.txt", "data": "pwned"},
		{"name": `a\..\..\escape.txt`, "data": "pwned"},
		{"name": "C:\\evil2.txt", "data": "pwned"},
	})
	dest := filepath.Join(t.TempDir(), "snap")
	res, err := snapshot.ExtractArchive(zipPath, "upload.zip", dest, limits())
	if err == nil {
		t.Fatalf("expected reject, got %+v", res)
	}
	// 任何违规条目 → 整次失败，半成品已清理
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatalf("partial dest should be removed")
	}
}

func TestSafeZipWithSymlinkRejected(t *testing.T) {
	zipPath := writeZip(t, []map[string]any{
		{"name": "link", "data": "/etc/passwd", "mode": uint32(os.ModeSymlink)},
	})
	dest := filepath.Join(t.TempDir(), "snap")
	if _, err := snapshot.ExtractArchive(zipPath, "upload.zip", dest, limits()); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestSafeZipHappyPathAndDeterministicHash(t *testing.T) {
	zipPathA := writeZip(t, []map[string]any{
		{"name": "src/app.py", "data": "print(1)\n"},
		{"name": "src/readme.md", "data": "docs\n"},
	})
	zipPathB := writeZip(t, []map[string]any{
		{"name": "src/readme.md", "data": "docs\n"},
		{"name": "src/app.py", "data": "print(1)\n"},
	})
	destA := filepath.Join(t.TempDir(), "snapA")
	destB := filepath.Join(t.TempDir(), "snapB")
	resA, err := snapshot.ExtractArchive(zipPathA, "upload.zip", destA, limits())
	if err != nil {
		t.Fatal(err)
	}
	resB, err := snapshot.ExtractArchive(zipPathB, "upload.zip", destB, limits())
	if err != nil {
		t.Fatal(err)
	}
	if resA.FileCount != 2 || resA.TotalSize != int64(len("print(1)\n")+len("docs\n")) {
		t.Fatalf("count/size %+v", resA)
	}
	if resA.ContentHash != resB.ContentHash {
		t.Fatalf("content hash must be order-independent: %s vs %s", resA.ContentHash, resB.ContentHash)
	}
	appPath := filepath.Join(destA, "src", "app.py")
	if _, err := os.Stat(appPath); err != nil {
		t.Fatal(err)
	}
}

func TestFileDirCollisionRejected(t *testing.T) {
	// 同一 zip：先写文件 backend-little，再写 backend-little/x —— 目录位置被文件占据
	zipPath := writeZip(t, []map[string]any{
		{"name": "backend-little", "data": "i am a file"},
		{"name": "backend-little/x", "data": "nested"},
	})
	_, err := snapshot.ExtractArchive(zipPath, "upload.zip", filepath.Join(t.TempDir(), "snap"), limits())
	if err == nil {
		t.Fatal("expected dir/file collision rejection")
	}
	if !strings.Contains(err.Error(), "conflicts") && !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLongSegmentRejected(t *testing.T) {
	seg := strings.Repeat("a", 300)
	zipPath := writeZip(t, []map[string]any{{"name": "src/" + seg + ".py", "data": "x"}})
	_, err := snapshot.ExtractArchive(zipPath, "upload.zip", filepath.Join(t.TempDir(), "snap"), limits())
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected segment-length rejection, got %v", err)
	}
}

func TestDeepNestedPathOKWithinLimits(t *testing.T) {
	// 深度约 30 层、总路径长会超 260（验证 longPath 前缀兜底）
	rel := ""
	for i := 0; i < 30; i++ {
		rel = path.Join(rel, fmt.Sprintf("seg%02d", i))
	}
	zipPath := writeZip(t, []map[string]any{{"name": path.Join(rel, "leaf.txt"), "data": "ok"}})
	dest := filepath.Join(t.TempDir(), "snap")
	// 让基础路径也不短
	deep := dest
	res, err := snapshot.ExtractArchive(zipPath, "upload.zip", deep, limits())
	if err != nil {
		t.Fatalf("deep extraction failed: %v", err)
	}
	if res.FileCount != 1 {
		t.Fatalf("count %d", res.FileCount)
	}
}


func TestDirEntryThenFileOK(t *testing.T) {
	// 用户实际场景：zip 含显式目录条目 backend-little/config/ 及其下文件
	zipPath := writeZip(t, []map[string]any{
		{"name": "backend-little/config/", "data": ""},
		{"name": "backend-little/config/app.js", "data": "console.log(1)\n"},
		{"name": "backend-little/config/readme.md", "data": "docs\n"},
	})
	res, err := snapshot.ExtractArchive(zipPath, "upload.zip", filepath.Join(t.TempDir(), "snap"), limits())
	if err != nil {
		t.Fatalf("dir-entry archive should extract: %v", err)
	}
	if res.FileCount != 2 {
		t.Fatalf("want 2 files, got %d", res.FileCount)
	}
	if _, err := os.Stat(filepath.Join(res.SnapshotPath, "backend-little", "config", "app.js")); err != nil {
		t.Fatalf("app.js missing: %v", err)
	}
}

func TestExtractedBudget(t *testing.T) {
	big := string(bytes.Repeat([]byte("x"), 4096))
	zipPath := writeZip(t, []map[string]any{{"name": "big.bin", "data": big}})
	l := limits()
	l.MaxFileBytes = 1024 // 单文件超限
	_, err := snapshot.ExtractArchive(zipPath, "upload.zip", filepath.Join(t.TempDir(), "snap"), l)
	if err == nil {
		t.Fatal("expected size limit rejection")
	}
}

func TestArchiveShaCoversWholeFile(t *testing.T) {
	data := bytes.Repeat([]byte("abc"), 1000)
	zipPath := writeZip(t, []map[string]any{{"name": "f.bin", "data": string(data)}})
	zipBytes := mustRead(t, zipPath)
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, sha, size, err := snapshot.HashArchive(f, limits())
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(zipBytes)) {
		t.Fatalf("size %d != %d", size, len(zipBytes))
	}
	want := sha256.Sum256(zipBytes)
	if sha != fmt.Sprintf("%x", want) {
		t.Fatal("sha mismatch")
	}
}

func TestTarGzHappyAndSymlink(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "src/app.py", Mode: 0o644, Typeflag: tar.TypeReg, Size: 5})
	_, _ = tw.Write([]byte("hello"))
	_ = tw.WriteHeader(&tar.Header{Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()

	tmp := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	// 有符号链接 → 拒绝
	_, err := snapshot.ExtractArchive(tmp, "a.tar.gz", filepath.Join(t.TempDir(), "snap"), limits())
	if err == nil {
		t.Fatal("expected symlink rejection in tar.gz")
	}

	// 正常 tar.gz
	buf.Reset()
	gz = gzip.NewWriter(&buf)
	tw = tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "src/app.py", Mode: 0o644, Typeflag: tar.TypeReg, Size: 5})
	_, _ = tw.Write([]byte("hello"))
	_ = tw.Close()
	_ = gz.Close()
	tmp2 := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := os.WriteFile(tmp2, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "snap2")
	res, err := snapshot.ExtractArchive(tmp2, "b.tar.gz", dest, limits())
	if err != nil {
		t.Fatal(err)
	}
	if res.FileCount != 1 {
		t.Fatalf("count %d", res.FileCount)
	}
}

