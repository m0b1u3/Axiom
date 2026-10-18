package scanner

import (
	"axiom/contracts"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityHomesSeparateAgentIdentityAndGeneration(t *testing.T) {
	data := t.TempDir()
	ref := contracts.SDKIdentityRef{ID: "1", Name: "Fixture", AgentID: "fixture-a", Generation: 1, Kind: "api_key"}
	first, e := SDKIdentityHome(data, ref)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []contracts.SDKIdentityRef{{ID: "2", Name: "Fixture", AgentID: "fixture-a", Generation: 1, Kind: "api_key"}, {ID: "1", Name: "Fixture", AgentID: "fixture-b", Generation: 1, Kind: "api_key"}, {ID: "1", Name: "Fixture", AgentID: "fixture-a", Generation: 2, Kind: "api_key"}} {
		p, e := SDKIdentityHome(data, v)
		if e != nil || p == first {
			t.Fatal("identity homes alias", e)
		}
	}
	st, e := os.Stat(first)
	if e != nil || st.Mode().Perm() != 0700 {
		t.Fatal("credential directory permissions", e)
	}
	if e = os.Remove(first); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(t.TempDir(), first); e != nil {
		t.Skip(e)
	}
	if _, e = SDKIdentityHome(data, ref); e == nil {
		t.Fatal("symlink credential directory accepted")
	}
}
func TestIdentityPreparationRejectsOtherNodeAndMissingSession(t *testing.T) {
	req := Request{TaskID: 42, Attempt: 1, AgentID: "fixture-a", DataDir: t.TempDir(), OutputPath: t.TempDir(), SDKIdentity: &contracts.SDKTaskIdentity{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "42", Attempt: 1}, Identity: contracts.SDKIdentityRef{ID: "1", Name: "Fixture", AgentID: "fixture-b", Generation: 1, Kind: "chatgpt"}}}
	if _, _, e := prepareTaskIdentity(req); e == nil {
		t.Fatal("other node accepted")
	}
	req.SDKIdentity.Identity.AgentID = "fixture-a"
	if _, _, e := prepareTaskIdentity(req); e == nil {
		t.Fatal("missing session accepted")
	}
	home, e := SDKIdentityHome(req.DataDir, req.SDKIdentity.Identity)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := contracts.EncodeJSON(req.SDKIdentity.Identity)
	os.WriteFile(filepath.Join(home, "session.json"), raw, 0600)
	os.MkdirAll(filepath.Join(req.OutputPath, "runtime"), 0700)
	if _, _, e := prepareTaskIdentity(req); e != nil {
		t.Fatal(e)
	}
	req.SDKIdentity.Execution.Attempt = 2
	if _, _, e := prepareTaskIdentity(req); e == nil {
		t.Fatal("other attempt accepted")
	}
}
