package render_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/code-audit-server/internal/render"
)

func TestHTMLToPDF(t *testing.T) {
	if render.FindBrowser() == "" {
		t.Skip("no Edge/Chrome browser found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pdf, err := render.HTMLToPDF(ctx, `<!doctype html><html><head><meta charset="utf-8"><style>body{font-family:sans-serif}</style></head><body><h1>PDF Test</h1></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pdf) < 1000 {
		t.Fatalf("pdf too small: %d", len(pdf))
	}
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("not a pdf: %s", string(pdf[:8]))
	}
}

func TestFindBrowserEnvOverride(t *testing.T) {
	// 无浏览器环境：FindBrowser 为空
	if os.Getenv("AXIOM_BROWSER") != "" {
		t.Skip("AXIOM_BROWSER overrides")
	}
	_ = render.FindBrowser() // 不崩即可
}
