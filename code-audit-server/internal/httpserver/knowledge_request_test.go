package httpserver

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKnowledgeRequestRejectsUnknownTrailingAndInvalidUtf8(t *testing.T) {
	for _, raw := range []string{`{"content":"ok","token":"fixture"}`, `{"content":"ok"} {}`, "{\"content\":\"" + string([]byte{255}) + "\"}"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		w := httptest.NewRecorder()
		var value struct {
			Content string `json:"content"`
		}
		if knowledgeBody(w, r, &value) || w.Code != 400 {
			t.Fatal("invalid document request accepted", w.Code)
		}
	}
}
