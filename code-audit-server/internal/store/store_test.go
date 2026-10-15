package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimAndFinish(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	id, err := st.InsertPending(ctx, Task{
		ScanNo:     "SCAN-test-1",
		SourcePath: "/src",
		Provider:   "openai",
		Model:      "gpt-5.6-sol",
		ScanMode:   "standard",
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := st.Claim(ctx, "agent-a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != id {
		t.Fatalf("claimed %+v", claimed)
	}
	empty, err := st.Claim(ctx, "agent-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if empty != nil {
		t.Fatalf("second claim should be empty, got %+v", empty)
	}

	line := 8
	if err := st.ReplaceFindings(ctx, id, []Finding{{
		ScanID:      id,
		Fingerprint: "fp-1",
		Title:       "cmdi",
		Severity:    "high",
		CWE:         []string{"CWE-78"},
		FilePath:    "app.py",
		StartLine:   &line,
		Description: "os.system",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, id, "partial", "provider_error", "models schema", 1); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "partial" || got.FindingCount != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestPromoteModelConfig(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	m, err := st.CreateModelConfig(ctx, ModelConfigRow{Name: "d", Provider: "deepseek", Model: "m", BaseURL: "u", ApiKeyEncrypted: "enc"})
	if err != nil {
		t.Fatal(err)
	}
	if m.CompatibilityStatus != "experimental" {
		t.Fatalf("initial %s", m.CompatibilityStatus)
	}
	if err := st.PromoteModelConfigToSupported(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetModelConfig(ctx, m.ID)
	if got.CompatibilityStatus != "supported" {
		t.Fatalf("promote failed %s", got.CompatibilityStatus)
	}
	// 不降级
	if err := st.PromoteModelConfigToSupported(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetModelConfig(ctx, m.ID)
	if got.CompatibilityStatus != "supported" {
		t.Fatal("regression")
	}
	// 空 configID 安全
	if err := st.PromoteModelConfigToSupported(ctx, 0); err != nil {
		t.Fatal(err)
	}
}

func TestCancelAndRetry(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	id, err := st.InsertPending(ctx, Task{
		ScanNo: "SCAN-cancel", SourcePath: "/src", Provider: "openai", Model: "gpt-5.6-sol",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 未领取的 pending 直接取消 → canceled
	if ok, _ := st.RequestCancel(ctx, id); !ok {
		t.Fatal("cancel pending failed")
	}
	got, _ := st.Get(ctx, id)
	if got.Status != "canceled" {
		t.Fatalf("pending cancel status=%s", got.Status)
	}
	// 重新入队（终态可重试）→ 领取后模拟扫描中再取消
	if ok, _ := st.Retry(ctx, id); !ok {
		t.Fatal("retry pending-cancel failed")
	}
	if _, err := st.Claim(ctx, "agent-a", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	// 扫描中 → 取消
	ok, err := st.RequestCancel(ctx, id)
	if err != nil || !ok {
		t.Fatalf("cancel ok=%v err=%v", ok, err)
	}
	got, _ = st.Get(ctx, id)
	if got.Status != "cancel_requested" {
		t.Fatalf("status=%s", got.Status)
	}
	// 取消后的心跳不能覆盖 cancel_requested
	if err := st.Heartbeat(ctx, id, "agent-a", 30*time.Second, "scanning", "{}"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(ctx, id)
	if got.Status != "cancel_requested" {
		t.Fatalf("heartbeat overwrote status=%s", got.Status)
	}
	// running 状态不允许 retry
	if ok, _ := st.Retry(ctx, id); ok {
		t.Fatal("retry should be rejected on running task")
	}
	// 终态后重试
	line := 1
	if err := st.ReplaceFindings(ctx, id, []Finding{{
		ScanID: id, Fingerprint: "fp-retry", Title: "stale", Severity: "high",
		StartLine: &line, Description: "should be deleted on retry",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, id, "canceled", "canceled", "user", 1); err != nil {
		t.Fatal(err)
	}
	ok, err = st.Retry(ctx, id)
	if err != nil || !ok {
		t.Fatalf("retry ok=%v err=%v", ok, err)
	}
	got, _ = st.Get(ctx, id)
	if got.Status != "pending" || got.AgentID != "" || got.FindingCount != 0 {
		t.Fatalf("after retry %+v", got)
	}
	left, err := st.ListFindings(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("retry left %d findings", len(left))
	}

	// 清出 pending 队列（id 又被重试回 pending），只留孤儿场景一条任务
	if ok, _ := st.RequestCancel(ctx, id); !ok {
		t.Fatal("clear pending failed")
	}
	// 上一任 Agent 崩溃留下的 cancel_requested 可被再领取，且标记 ClaimedFrom
	cid, err := st.InsertPending(ctx, Task{
		ScanNo: "SCAN-cancel-orphan", SourcePath: "/src", Provider: "openai", Model: "gpt-5.6-sol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(ctx, "agent-dead", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.RequestCancel(ctx, cid); !ok {
		t.Fatal("cancel claimed failed")
	}
	// 让 lease 过期后 agent-b 接管
	if err := st.Heartbeat(ctx, cid, "agent-dead", 0, "scanning", "{}"); err != nil {
		t.Fatal(err)
	}
	// 直接置过期
	if _, err := st.db.ExecContext(ctx, `UPDATE scan_tasks SET lease_expires_at = '2000-01-01T00:00:00Z' WHERE id = ?`, cid); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := st.Claim(ctx, "agent-b", 30*time.Second)
	if err != nil || reclaimed == nil {
		t.Fatalf("reclaim orphan cancel; err=%v", err)
	}
	if reclaimed.ClaimedFrom != "cancel_requested" {
		t.Fatalf("ClaimedFrom=%s", reclaimed.ClaimedFrom)
	}
}
