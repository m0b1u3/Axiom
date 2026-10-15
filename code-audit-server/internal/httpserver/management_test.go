package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestExecutionSettingsAuthorization(t *testing.T) {
	st, e := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	_, e = st.EnsureUser(t.Context(), "reader", auth.HashPassword("test", "pass"))
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer((&httpserver.Server{Store: st, Secret: "test"}).Routes())
	defer ts.Close()
	token := login(t, ts.URL, "reader", "pass")
	for _, v := range []struct {
		token string
		want  int
	}{{"", 401}, {token, 403}} {
		r, _ := http.NewRequest("PUT", ts.URL+"/api/v1/settings/execution", bytes.NewBufferString(`{"global":2,"perModel":2,"perProject":2,"revision":1}`))
		r.Header.Set("Authorization", "Bearer "+v.token)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != v.want {
			t.Fatalf("got %d want %d", resp.StatusCode, v.want)
		}
	}
}
