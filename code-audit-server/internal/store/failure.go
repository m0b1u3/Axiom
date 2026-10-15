package store

import (
	"regexp"
	"strings"
)

var rateLimitPattern = regexp.MustCompile(`(?i)(\b429\b|rate.?limit|too many requests|quota exceeded|insufficient_quota)`)
var quotaPattern = regexp.MustCompile(`(?i)(free quota exhausted|quota_exhausted|insufficient_quota|use free tier only|insufficient (balance|credit))`)
var authPattern = regexp.MustCompile(`(?i)(\b401\b|\b403\b|invalid.?api.?key|incorrect api key|authentication failed|unauthorized)`)
var networkPattern = regexp.MustCompile(`(?i)(ECONNRESET|ECONNREFUSED|ENOTFOUND|EAI_AGAIN|fetch failed|connection reset|connection refused|\b50[234]\b|service unavailable)`)

// Classification uses terminal errors only; source-code/log text must never set task status.
// The original error message is retained for diagnosis.
func ClassifyFailure(code, message string) string {
	switch code {
	case "quota_exhausted", "canceled", "timeout", "lease_expired", "configuration_error", "invalid_result", "rate_limit", "authentication_error", "provider_unavailable":
		return code
	}
	switch {
	case strings.Contains(strings.ToLower(message), "missing environment variable"):
		return "configuration_error"
	case quotaPattern.MatchString(message):
		return "quota_exhausted"
	case rateLimitPattern.MatchString(message):
		return "rate_limit"
	case authPattern.MatchString(message):
		return "authentication_error"
	case networkPattern.MatchString(message):
		return "provider_unavailable"
	case strings.Contains(strings.ToLower(message), "timed out") || strings.Contains(strings.ToLower(message), "deadline exceeded"):
		return "timeout"
	}
	return code
}
