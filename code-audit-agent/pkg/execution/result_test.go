package execution

import (
	"axiom/contracts"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompletionHTTPErrors(t *testing.T) {
	for _, test := range []struct {
		body  string
		want  error
		retry bool
	}{
		{`{"code":40900,"data":{"error":{"code":"lease_expired","message":"fixture","retryable":false}}}`, ErrLeaseLost, false},
		{`{"code":40900,"data":{"error":{"code":"completion_conflict","message":"fixture","retryable":false}}}`, ErrCompletionConflict, false},
		{`{"code":40903,"data":{"error":{"code":"configuration_error","message":"fixture","retryable":false}}}`, nil, false},
		{`{"code":40901}`, ErrLeaseLost, false},
		{`{"code":40902}`, ErrCompletionConflict, false},
	} {
		t.Run(test.body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409); w.Write([]byte(test.body)) }))
			defer s.Close()
			e := NewClient(s.URL, "fixture").call(context.Background(), "complete", struct{}{}, nil)
			if test.want != nil {
				if !errors.Is(e, test.want) {
					t.Fatal(e)
				}
			} else {
				var p *ProtocolError
				if !errors.As(e, &p) || p.Code != "configuration_error" || p.Retryable {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestProtocolErrorRetainsOriginRetryAndMessage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte(`{"code":50300,"data":{"error":{"code":"provider_unavailable","origin":"server","message":"fixture database unavailable","retryable":true}}}`))
	}))
	defer s.Close()
	err := NewClient(s.URL, "fixture").call(context.Background(), "heartbeat", struct{}{}, nil)
	var p *ProtocolError
	if !errors.As(err, &p) || p.Code != contracts.ErrorCodeProviderUnavailable || p.Origin != contracts.ErrorOriginServer || !p.Retryable || p.Message != "fixture database unavailable" {
		t.Fatal("structured protocol error lost details", err)
	}
}
