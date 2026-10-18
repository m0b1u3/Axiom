package scanner

import (
	"axiom/contracts"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeMaterializationPreservesFrozenBytesAndRefusesTampering(t *testing.T) {
	root := t.TempDir()
	bad := contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{{VersionID: "../escape", Title: "fixture", Content: "text", SHA256: fmt.Sprintf("%064d", 0)}}}
	if e := materializeKnowledge(t.TempDir(), bad); e == nil {
		t.Fatal("unsafe knowledge filename accepted")
	}

	body := "# 合成知识\n第二行\n"
	h := sha256.Sum256([]byte(body))
	v := contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{{VersionID: "17", Title: "合成知识", Content: body, SHA256: fmt.Sprintf("%x", h)}}}
	if e := materializeKnowledge(root, v); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(root, "runtime/knowledge/17.md")
	raw, e := os.ReadFile(file)
	if e != nil || string(raw) != body {
		t.Fatal(e, string(raw))
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("knowledge permissions", info.Mode())
	}
	if e = materializeKnowledge(root, v); e == nil {
		t.Fatal("existing execution materialization overwritten")
	}
	v.Knowledge[0].Content = "changed"
	if e = materializeKnowledge(t.TempDir(), v); e == nil {
		t.Fatal("tampered knowledge accepted")
	}
}
