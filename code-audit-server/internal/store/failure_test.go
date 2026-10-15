package store

import "testing"

func TestFailureClassification(t *testing.T) {
	for _, v := range []struct{ code, msg, want string }{
		{"scanner_error", "HTTP 429 too many requests", "rate_limit"}, {"scanner_error", "401 invalid api key", "authentication_error"}, {"scanner_error", "fetch failed ECONNRESET", "provider_unavailable"}, {"scanner_error", "request timed out", "timeout"}, {"configuration_error", "HTTP 429", "configuration_error"}, {"scanner_error", "audit detected SQL injection", "scanner_error"},
	} {
		if got := ClassifyFailure(v.code, v.msg); got != v.want {
			t.Errorf("%s: got %s want %s", v.msg, got, v.want)
		}
	}
}

func TestQuotaExhaustedIsNotTemporaryRateLimit(t *testing.T) {
	if got := ClassifyFailure("scanner_error", "429 Free quota exhausted. use free tier only"); got != "quota_exhausted" {
		t.Fatal(got)
	}
}
