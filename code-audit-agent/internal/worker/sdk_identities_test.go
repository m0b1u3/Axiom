package worker

import (
	"axiom/code-audit-agent/internal/config"
	"axiom/code-audit-agent/pkg/execution"
	"axiom/contracts"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdentityLeaseLossStopsNativeProcessUsingServerLease(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("Node required")
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "dist"), 0700)
	stopped := filepath.Join(root, "stopped.json")
	encoded, _ := json.Marshal(stopped)
	// A synthetic JSONL producer, never the SDK. It waits indefinitely until the
	// control-plane lease deadline cancels its process group.
	script := `const fs=require('node:fs');const c=JSON.parse(fs.readFileSync(process.argv[3],'utf8'));const o=c.operation;const v={schemaVersion:1,operationId:o.id,identityId:o.identity.id,agentId:o.identity.agentId,generation:o.identity.generation,leaseToken:c.leaseToken,sequence:1,status:'running',authenticated:null,instructions:null,error:null};console.log(JSON.stringify(v));v.sequence=2;v.status='waiting';v.instructions={verificationUrl:'https://auth.openai.com/codex/device',userCode:'MOCK-1234',expiresAt:o.deadline};console.log(JSON.stringify(v));process.on('SIGTERM',()=>{fs.writeFileSync(` + string(encoded) + `,JSON.stringify({fixtureOnly:true,canceled:true}));process.exit(0)});setInterval(()=>{},1000);`
	if e := os.WriteFile(filepath.Join(root, "dist/main.js"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	claim := contracts.SDKIdentityClaim{SchemaVersion: 1, LeaseSeconds: 10, LeaseToken: strings.Repeat("a", 64), Operation: contracts.SDKIdentityOperation{SchemaVersion: 1, ID: "1", Identity: contracts.SDKIdentityRef{ID: "1", Name: "Fixtures / Mock lease", AgentID: "fixture-agent", Generation: 2, Kind: "chatgpt"}, Action: "login_device_code", Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339), Deadline: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}}
	var waiting atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/claim") {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": claim})
			return
		}
		var v contracts.SDKIdentityReport
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			t.Error(e)
			return
		}
		if v.Status == "waiting" && !waiting.Swap(true) {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"status": "waiting"}})
			return
		}
		if waiting.Load() && v.Status == "waiting" {
			<-r.Context().Done()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"status": string(v.Status)}})
	}))
	defer server.Close()
	client := execution.NewClient(server.URL, "fixture-token")
	client.SDKIdentityVersion = 1
	worker := Worker{Cfg: config.Config{AgentID: "fixture-agent", DataDir: filepath.Join(root, "data"), ScannerDir: root, NodePath: node, Lease: 120 * time.Second}}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	start := time.Now()
	handled, e := worker.executeIdentity(ctx, client)
	if !handled || e == nil {
		t.Fatal("lost identity lease did not interrupt")
	}
	if elapsed := time.Since(start); elapsed >= 10*time.Second {
		t.Fatal("process outlived the Server lease", elapsed)
	}
	if _, e := os.Stat(stopped); e != nil {
		t.Fatal("native device process was not canceled", e)
	}
}
