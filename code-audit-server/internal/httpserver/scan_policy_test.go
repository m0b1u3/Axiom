package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDeepPolicyValidation(t *testing.T) {
	st, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("test", "pass"))
	ts := httptest.NewServer((&httpserver.Server{Store: st, Secret: "test"}).Routes())
	defer ts.Close()
	token := login(t, ts.URL, "admin", "pass")
	for _, v := range []struct {
		mode string
		runs any
		want int
	}{{"standard", 4, 400}, {"deep", -1, 400}, {"deep", 1001, 200}, {"deep", 9007199254740991, 200}, {"deep", 9007199254740992, 400}, {"deep", 1.5, 400}, {"deep", 4, 200}, {"deep", 0, 400}, {"deep", nil, 200}} {
		code, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourcePath": "/src", "scanMode": v.mode, "deepMaxDiscoveryRuns": v.runs, "timeoutMinutes": 3})
		if code != v.want {
			t.Fatalf("mode=%s runs=%v got=%d body=%s", v.mode, v.runs, code, body)
		}
	}
}

func TestExploitabilityPolicyValidation(t *testing.T) {
	st, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	st.EnsureAdministrator(t.Context(), "admin", auth.HashPassword("test", "pass"))
	ts := httptest.NewServer((&httpserver.Server{Store: st, Secret: "test"}).Routes())
	defer ts.Close()
	token := login(t, ts.URL, "admin", "pass")
	for _, v := range []struct {
		mode, validation, context string
		want                      int
	}{
		{"standard", "exploitability", "普通用户可注册", 200}, {"deep", "exploitability", "", 400}, {"standard", "invalid", "", 400}, {"standard", "default", "用户背景", 400}, {"deep", "default", "", 200},
	} {
		code, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourcePath": "/src", "scanMode": v.mode, "validationMode": v.validation, "businessContext": v.context})
		if code != v.want {
			t.Fatalf("%+v got %d %s", v, code, body)
		}
	}
}
