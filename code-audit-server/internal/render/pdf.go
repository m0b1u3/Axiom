package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// 候选浏览器路径（Windows）。第一个存在的被使用。
var browserCandidates = []string{
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	`C:\Users\Administrator\AppData\Local\Google\Chrome\Application\chrome.exe`,
}

// FindBrowser 返回可用的 headless 浏览器路径；可被环境变量 AXIOM_BROWSER 覆盖。
func FindBrowser() string {
	if v := os.Getenv("AXIOM_BROWSER"); v != "" {
		if fileExists(v) {
			return v
		}
	}
	if runtime.GOOS == "darwin" {
		for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"} {
			if fileExists(p) {
				return p
			}
		}
		return ""
	}
	if runtime.GOOS == "linux" {
		for _, p := range []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"/snap/bin/chromium",
		} {
			if fileExists(p) {
				return p
			}
		}
		return ""
	}
	for _, p := range browserCandidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// HTMLToPDF 用 headless 浏览器把 HTML 转成 PDF。html 必须是自包含（内联样式）文档。
func HTMLToPDF(ctx context.Context, html string) ([]byte, error) {
	browser := FindBrowser()
	if browser == "" {
		return nil, errors.New("no PDF browser available (install Edge/Chrome or set AXIOM_BROWSER)")
	}
	dir := os.TempDir()
	htmlPath := filepath.Join(dir, fmt.Sprintf("axiom-report-%d.html", time.Now().UnixNano()))
	pdfPath := filepath.Join(dir, fmt.Sprintf("axiom-report-%d.pdf", time.Now().UnixNano()))
	defer os.Remove(htmlPath)
	defer os.Remove(pdfPath)
	if err := os.WriteFile(htmlPath, []byte(html), 0o600); err != nil {
		return nil, err
	}

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--no-pdf-header-footer",
		"--print-to-pdf=" + pdfPath,
		"file://" + filepath.ToSlash(htmlPath),
	}
	cmd := exec.CommandContext(ctx, browser, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("browser pdf failed: %w (%s)", err, truncate(string(out), 300))
	}
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("pdf not produced: %w", err)
	}
	return pdf, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
