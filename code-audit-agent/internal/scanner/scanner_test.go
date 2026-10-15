package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadNormalizedAndProgress(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "normalized"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{
		"taskId":   1,
		"status":   "completed",
		"findings": []any{},
	}
	raw, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, LayoutNormalized), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	progress := map[string]any{"taskId": 1, "phase": "completed", "updatedAt": "t"}
	praw, _ := json.Marshal(progress)
	if err := os.WriteFile(filepath.Join(dir, LayoutProgress), praw, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadNormalized(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" {
		t.Fatalf("%+v", got)
	}
	p, err := ReadProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Phase != "completed" {
		t.Fatalf("%+v", p)
	}
}

func TestChildEnvInjectOverride(t *testing.T) {
	// 模拟进程 env 已有 DEEPSEEK_API_KEY（旧值）+ 注入新值 → 子进程只保留注入值
	t.Setenv("DEEPSEEK_API_KEY", "old-key")
	t.Setenv("OPENAI_API_KEY", "ambient")
	env := childEnvForCredential("deepseek", "DEEPSEEK_API_KEY", false, map[string]string{"DEEPSEEK_API_KEY": "new-key"})
	var got string
	for _, kv := range env {
		if kv == "DEEPSEEK_API_KEY=new-key" {
			got = kv
		}
		if kv == "DEEPSEEK_API_KEY=old-key" {
			t.Fatal("old key must be replaced")
		}
		if kv == "OPENAI_API_KEY=ambient" {
			t.Fatal("ambient openai key must be stripped for non-openai provider")
		}
	}
	if got == "" {
		t.Fatal("injected key missing from child env")
	}
}
