package httpserver_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
)

func TestLoginCreateProjectAndScan(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	token := login(t, ts.URL, "admin", "admin")
	if token == "" {
		t.Fatal("empty token")
	}

	code, body := doJSON(t, ts.URL+"/api/v1/projects", token, map[string]any{"name": "demo"})
	if code != 200 {
		t.Fatalf("create project %d %s", code, body)
	}

	code, body = doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{
		"sourcePath": filepath.Join(dir, "src"),
		"provider":   "openai",
		"model":      "gpt-5.6-sol",
	})
	if code != 200 {
		t.Fatalf("create scan %d %s", code, body)
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Status != "pending" {
		t.Fatalf("expected pending, got %s (%s)", env.Data.Status, body)
	}

	code, _ = doJSON(t, ts.URL+"/api/v1/scans", "", map[string]any{"sourcePath": "x"})
	if code != 401 {
		t.Fatalf("unauth create scan want 401 got %d", code)
	}

	// cancel pending -> 没有进程在跑，直接终态 canceled
	code, body = doJSON(t, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", env.Data.ID)+"/cancel", token, map[string]any{})
	if code != 200 {
		t.Fatalf("cancel want 200 got %d %s", code, body)
	}
	var canceled struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &canceled); err != nil {
		t.Fatal(err)
	}
	if canceled.Data.Status != "canceled" {
		t.Fatalf("pending cancel status=%s", canceled.Data.Status)
	}
	// canceled 是终态 → retry 200 回到 pending
	code, body = doJSON(t, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", env.Data.ID)+"/retry", token, map[string]any{})
	if code != 200 {
		t.Fatalf("retry terminal want 200 got %d %s", code, body)
	}
	// 现在的 pending 是 running → retry 409
	code, body = doJSON(t, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", env.Data.ID)+"/retry", token, map[string]any{})
	if code != 409 {
		t.Fatalf("retry on running want 409 got %d %s", code, body)
	}
	// 直接置 claimed（模拟已领取）→ cancel 置 cancel_requested
	if err := st.Finish(t.Context(), env.Data.ID, "canceled", "canceled", "user", 0); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.Retry(t.Context(), env.Data.ID); err != nil || !ok {
		t.Fatalf("retry setup ok=%v err=%v", ok, err)
	}
	if _, err := st.Claim(t.Context(), "agent-x", time.Minute); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", env.Data.ID)+"/cancel", token, map[string]any{})
	if code != 200 {
		t.Fatalf("cancel claimed want 200 got %d %s", code, body)
	}
	var c struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	if c.Data.Status != "cancel_requested" {
		t.Fatalf("claimed cancel status=%s", c.Data.Status)
	}
}

func TestUploadSnapshot(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	snapDir := filepath.Join(dir, "snapshots")
	srv := &httpserver.Server{
		Store: st, Secret: secret, SnapshotDir: snapDir,
		Limits: func() snapshot.Limits {
			l := snapshot.DefaultLimits()
			l.MaxArchiveBytes = 10 << 20
			return l
		}(),
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	// 构造合法 zip：src/app.py
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create("src/app.py")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("print(1)\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "src.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(zipBuf.Bytes())
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("upload status %d: %s", res.StatusCode, raw)
	}
	var env struct {
		Data struct {
			ID               int64  `json:"id"`
			ArchiveSha256    string `json:"archiveSha256"`
			SnapshotPath     string `json:"snapshotPath"`
			FileCount        int    `json:"fileCount"`
			OriginalFilename string `json:"originalFilename"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.FileCount != 1 || len(env.Data.ArchiveSha256) != 64 {
		t.Fatalf("upload resp %+v", env.Data)
	}
	if _, err := os.Stat(filepath.Join(env.Data.SnapshotPath, "src", "app.py")); err != nil {
		t.Fatal(err)
	}
	// 再传一次 → 同内容去重，ID 相同
	body.Reset()
	mw = multipart.NewWriter(&body)
	fw, _ = mw.CreateFormFile("file", "src.zip")
	_, _ = fw.Write(zipBuf.Bytes())
	_ = mw.Close()
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	raw2, _ := io.ReadAll(res2.Body)
	var env2 struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw2, &env2)
	if env2.Data.ID != env.Data.ID {
		t.Fatalf("dedupe failed: %d vs %d", env2.Data.ID, env.Data.ID)
	}

	// 用快照 id 创建扫描 → 服务端取不可变路径
	code, respBody := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{
		"sourceSnapshotId": env.Data.ID,
		"provider":         "openai",
		"model":            "gpt-5.6-sol",
	})
	if code != 200 {
		t.Fatalf("create scan by snapshot want 200 got %d %s", code, respBody)
	}
	var bySnap struct {
		Data struct {
			Status           string `json:"status"`
			SourcePath       string `json:"sourcePath"`
			SourceSnapshotID int64  `json:"sourceSnapshotId"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(respBody), &bySnap)
	if bySnap.Data.SourcePath != env.Data.SnapshotPath || bySnap.Data.SourceSnapshotID != env.Data.ID {
		t.Fatalf("by-snapshot scan %+v", bySnap.Data)
	}
	// 不存在的快照 → 400
	code, _ = doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": 99999, "provider": "openai"})
	if code != 400 {
		t.Fatalf("missing snapshot want 400 got %d", code)
	}
	// 非法归档（带 ../escape）→ 400
	zipBuf.Reset()
	zw = zip.NewWriter(&zipBuf)
	w, _ = zw.Create("../evil.txt")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	body.Reset()
	mw = multipart.NewWriter(&body)
	fw, _ = mw.CreateFormFile("file", "evil.zip")
	_, _ = fw.Write(zipBuf.Bytes())
	_ = mw.Close()
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != 400 {
		t.Fatalf("evil upload want 400 got %d", res3.StatusCode)
	}
}

func TestFindingStatusAndCompare(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret, SnapshotDir: filepath.Join(dir, "snapshots"), Limits: snapshot.DefaultLimits()}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	// 两次扫描（同快照），第二次比第一次多一条 finding
	snapID, err := st.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "x.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: strings.Repeat("b", 64), SnapshotPath: filepath.Join(dir, "snap"), FileCount: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = snapID
	_, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{
		"sourceSnapshotId": snapID.ID, "provider": "openai", "model": "gpt-5.6-sol",
	})
	var scan1 struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &scan1)
	_, body = doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{
		"sourceSnapshotId": snapID.ID, "provider": "openai", "model": "gpt-5.6-sol",
	})
	var scan2 struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &scan2)

	line := 1
	if err := st.ReplaceFindings(t.Context(), scan1.Data.ID, []store.Finding{{
		ScanID: scan1.Data.ID, Fingerprint: "fp-A", Title: "A", Severity: "high",
		FilePath: "a.py", StartLine: &line, Description: "a",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), scan1.Data.ID, "completed", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceFindings(t.Context(), scan2.Data.ID, []store.Finding{
		{ScanID: scan2.Data.ID, Fingerprint: "fp-A", Title: "A", Severity: "high", FilePath: "a.py", StartLine: &line, Description: "a"},
		{ScanID: scan2.Data.ID, Fingerprint: "fp-B", Title: "B", Severity: "medium", FilePath: "b.py", StartLine: &line, Description: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), scan2.Data.ID, "completed", "", "", 2); err != nil {
		t.Fatal(err)
	}

	findings2, _ := st.ListFindings(t.Context(), scan2.Data.ID)
	fpB := findings2[1].ID

	// PUT 状态
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/findings/"+fmt.Sprintf("%d", fpB)+"/status", strings.NewReader(`{"status":"false_positive"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("set status want 200 got %d", res.StatusCode)
	}
	after, _ := st.ListFindings(t.Context(), scan2.Data.ID)
	if after[1].Status != "false_positive" {
		t.Fatalf("status=%s", after[1].Status)
	}
	// 非法状态 400
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/v1/findings/"+fmt.Sprintf("%d", fpB)+"/status", strings.NewReader(`{"status":"evil"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, _ = http.DefaultClient.Do(req)
	_ = res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("bad status want 400 got %d", res.StatusCode)
	}

	// 对比：added=B，removed=0，unchanged=1
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", scan2.Data.ID)+"/compare", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var cmp struct {
		Data struct {
			PreviousScanID int64 `json:"previousScanId"`
			Added          []any `json:"added"`
			Removed        []any `json:"removed"`
			Unchanged      int   `json:"unchanged"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &cmp)
	if cmp.Data.PreviousScanID != scan1.Data.ID || len(cmp.Data.Added) != 1 || len(cmp.Data.Removed) != 0 || cmp.Data.Unchanged != 1 {
		t.Fatalf("compare %+v", cmp.Data)
	}
}

func TestDashboard(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject(t.Context(), "p1", 1); err != nil {
		t.Fatal(err)
	}
	line := 1
	id, err := st.InsertPending(t.Context(), store.Task{ScanNo: "SCAN-d", SourcePath: "/src", Provider: "openai", Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceFindings(t.Context(), id, []store.Finding{
		{ScanID: id, Fingerprint: "f1", Title: "a", Severity: "critical", FilePath: "a.py", StartLine: &line, Description: "a"},
		{ScanID: id, Fingerprint: "f2", Title: "b", Severity: "high", FilePath: "b.py", StartLine: &line, Description: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), id, "completed", "", "", 2); err != nil {
		t.Fatal(err)
	}

	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("dashboard status %d body %.200s", res.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Stats struct {
				Projects     int64   `json:"projects"`
				Scans        int64   `json:"scans"`
				Critical     int64   `json:"critical"`
				High         int64   `json:"high"`
				Running      int64   `json:"running"`
				OpenFindings int64   `json:"openFindings"`
				CriticalOpen int64   `json:"criticalOpen"`
				SuccessRate  float64 `json:"successRate"`
			} `json:"stats"`
			Recent      []any `json:"recent"`
			LatestDelta int64 `json:"latestDelta"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Data.Stats.Scans != 1 || env.Data.Stats.Critical != 1 || env.Data.Stats.High != 1 ||
		env.Data.Stats.Running != 0 || env.Data.Stats.Projects != 1 || len(env.Data.Recent) != 1 {
		t.Fatalf("dashboard %+v", env.Data)
	}
	if env.Data.Stats.OpenFindings != 2 || env.Data.Stats.CriticalOpen != 1 {
		t.Fatalf("risk stats %+v", env.Data.Stats)
	}
	if env.Data.Stats.SuccessRate != 100 {
		t.Fatalf("success rate %v", env.Data.Stats.SuccessRate)
	}
}

func TestFindingSummaries(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	line := 1
	// 两次扫描同一 fingerprint fp-A（第二次 high，状态 default open）+ 一次 fp-B
	id1, _ := st.InsertPending(t.Context(), store.Task{ScanNo: "S1", SourcePath: "/src", Provider: "openai", Model: "m"})
	_ = st.ReplaceFindings(t.Context(), id1, []store.Finding{
		{ScanID: id1, Fingerprint: "fp-A", Title: "A", Severity: "medium", FilePath: "a.py", StartLine: &line, Description: "a"},
	})
	_ = st.Finish(t.Context(), id1, "completed", "", "", 1)
	id2, _ := st.InsertPending(t.Context(), store.Task{ScanNo: "S2", SourcePath: "/src", Provider: "openai", Model: "m"})
	_ = st.ReplaceFindings(t.Context(), id2, []store.Finding{
		{ScanID: id2, Fingerprint: "fp-A", Title: "A newer", Severity: "high", FilePath: "a.py", StartLine: &line, Description: "a"},
		{ScanID: id2, Fingerprint: "fp-B", Title: "B", Severity: "critical", FilePath: "b.py", StartLine: &line, Description: "b"},
	})
	_ = st.Finish(t.Context(), id2, "completed", "", "", 2)
	if ok, err := st.SetFindingStatus(t.Context(), 1, "confirmed"); err != nil || !ok {
		t.Fatalf("set status ok=%v err=%v", ok, err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/findings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var env struct {
		Data []struct {
			Fingerprint string `json:"fingerprint"`
			Severity    string `json:"severity"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			ScanID      int64  `json:"scanId"`
			FirstSeen   int64  `json:"firstSeenScanId"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &env)
	if len(env.Data) != 2 {
		t.Fatalf("want 2 summaries (deduped), got %d", len(env.Data))
	}
	// 按 severity 排序：critical 在前
	if env.Data[0].Fingerprint != "fp-B" {
		t.Fatalf("order wrong: %+v", env.Data[0])
	}
	// fp-A 取最近一次出现（high, scan2, firstSeen=scan1）
	var a *struct {
		Fingerprint string `json:"fingerprint"`
		Severity    string `json:"severity"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		ScanID      int64  `json:"scanId"`
		FirstSeen   int64  `json:"firstSeenScanId"`
	}
	for i := range env.Data {
		if env.Data[i].Fingerprint == "fp-A" {
			a = &env.Data[i]
		}
	}
	if a == nil || a.Severity != "high" || a.Title != "A newer" || a.ScanID != id2 || a.FirstSeen != id1 {
		t.Fatalf("fp-A summary wrong: %+v", a)
	}

	// 状态过滤
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/findings?status=confirmed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, _ = http.DefaultClient.Do(req)
	raw2, _ := io.ReadAll(res.Body)
	var env2 struct {
		Data []struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw2, &env2)
	if len(env2.Data) != 1 || env2.Data[0].Fingerprint != "fp-A" {
		t.Fatalf("status filter wrong: %+v", env2.Data)
	}
}

func TestModelConfigs(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	// 创建
	code, body := doJSON(t, ts.URL+"/api/v1/model-configs", token, map[string]any{
		"name": "本地 DeepSeek", "provider": "deepseek",
		"baseUrl": "https://api.deepseek.com", "model": "deepseek-v4-flash-vision-exp",
		"apiKey": "sk-abc-123",
	})
	if code != 200 {
		t.Fatalf("create want 200 got %d %s", code, body)
	}
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &created)

	do := func(method, url string, payload any) (int, string) {
		var r io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, url, r)
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}

	// 列表不回传密钥
	code, body = do("GET", ts.URL+"/api/v1/model-configs", nil)
	var list struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &list)
	if code != 200 || len(list.Data) != 1 {
		t.Fatalf("list %d %v", code, list.Data)
	}
	if _, has := list.Data[0]["apiKey"]; has {
		t.Fatal("apiKey must not leak in list")
	}

	// 删除
	code, _ = do("DELETE", ts.URL+"/api/v1/model-configs/"+fmt.Sprintf("%d", created.Data.ID), nil)
	if code != 200 {
		t.Fatalf("delete want 200 got %d", code)
	}
	code, body = do("GET", ts.URL+"/api/v1/model-configs", nil)
	var empty struct {
		Data []any `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &empty)
	if len(empty.Data) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(empty.Data))
	}
}

func TestScanLogs(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	id, err := st.InsertPending(t.Context(), store.Task{ScanNo: "S", SourcePath: "/src", Provider: "deepseek", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "scan-out")
	_ = st.SetOutputPath(t.Context(), id, out)
	logDir := filepath.Join(out, "logs")
	_ = os.MkdirAll(logDir, 0o700)
	lines := `{"timestamp":"t1","level":"info","kind":"progress","message":"phase=discovery"}
{"timestamp":"t2","level":"info","kind":"activity","message":"running rg"}
{"timestamp":"t3","level":"warn","kind":"warning","message":"low coverage"}
`
	if err := os.WriteFile(filepath.Join(logDir, "events.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := func(after int) (int, string) {
		url := ts.URL + "/api/v1/scans/" + fmt.Sprintf("%d", id) + "/logs?after=" + strconv.Itoa(after)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}

	code, body := resp(0)
	if code != 200 {
		t.Fatalf("logs want 200 got %d", code)
	}
	var env struct {
		Data struct {
			Events []struct {
				N       int    `json:"n"`
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"events"`
			NextAfter int `json:"nextAfter"`
			Total     int `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &env)
	if len(env.Data.Events) != 3 || env.Data.NextAfter != 3 || env.Data.Total != 3 {
		t.Fatalf("logs %+v", env.Data)
	}
	if env.Data.Events[1].Kind != "activity" {
		t.Fatalf("event1 %+v", env.Data.Events[1])
	}
	// 增量：after=2 只回第 3 条
	code, body = resp(2)
	_ = json.Unmarshal([]byte(body), &env)
	if len(env.Data.Events) != 1 || env.Data.Events[0].N != 3 {
		t.Fatalf("incremental %+v", env.Data.Events)
	}
}

func TestDeleteScan(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	// 插入并完成一条任务
	id, err := st.InsertPending(t.Context(), store.Task{ScanNo: "S-del", SourcePath: "/src", Provider: "deepseek", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Finish(t.Context(), id, "completed", "", "", 0)

	// 运行中/未完成状态删除 → 409：重新排队后 claim 出 running 态
	if ok, _ := st.Retry(t.Context(), id); !ok {
		t.Fatal("retry failed")
	}
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", id), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, _ := http.DefaultClient.Do(req)
	_ = res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatalf("running delete want 409 got %d", res.StatusCode)
	}

	// 完成态删除 → 200；再查 404
	_ = st.Finish(t.Context(), id, "completed", "", "", 0)
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", id), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rawDel, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("delete want 200 got %d %s", res.StatusCode, rawDel)
	}
	if _, err := st.Get(t.Context(), id); err == nil {
		t.Fatal("scan still exists after delete")
	}
	findings, _ := st.ListFindings(t.Context(), id)
	if len(findings) != 0 {
		t.Fatal("findings not cleaned")
	}
}

func TestReportExport(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret := "test-secret"
	if _, err := st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "admin")); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: st, Secret: secret}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := login(t, ts.URL, "admin", "admin")

	_, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{
		"sourcePath": filepath.Join(dir, "src"),
		"provider":   "openai",
		"model":      "gpt-5.6-sol",
	})
	var env struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), env.Data.ID, "partial", "", "", 0); err != nil {
		t.Fatal(err)
	}
	line := 8
	if err := st.ReplaceFindings(t.Context(), env.Data.ID, []store.Finding{{
		ScanID: env.Data.ID, Fingerprint: "fp", Title: "cmdi", Severity: "high",
		CWE: []string{"CWE-78"}, FilePath: "app.py", StartLine: &line, Description: "os.system",
	}}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/scans/"+fmt.Sprintf("%d", env.Data.ID)+"/report", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("report status %d", res.StatusCode)
	}
	if !strings.Contains(string(raw), "扫描报告") || !strings.Contains(string(raw), "CWE-78") {
		t.Fatalf("unexpected report body")
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content type %s", ct)
	}
}

func login(t *testing.T, base, user, pass string) string {
	t.Helper()
	_, body := doJSON(t, base+"/api/v1/auth/login", "", map[string]any{"username": user, "password": pass})
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data.Token
}

func doJSON(t *testing.T, url, token string, payload any) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.String()
}
