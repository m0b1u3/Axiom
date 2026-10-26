package contracts

import (
	"regexp"
	"strings"
	"unicode"
)

var validationComment = regexp.MustCompile(`(?s)<!--.*?-->`)
var validationHeading = regexp.MustCompile(`^#{1,6}(?:[ \t]+|$)`)
var validationHTMLHeading = regexp.MustCompile(`(?i)^<h[1-6](?:[ \t][^>]*)?>.*</h[1-6]>$`)
var validationInlineSection = regexp.MustCompile(`[ \t]#{2,6}[ \t]+\S`)
var validationQuote = regexp.MustCompile(`^(?:>[ \t]*)+`)
var validationFence = regexp.MustCompile("^(?:`{3,}|~{3,})[^`~]*$")
var validationRule = regexp.MustCompile(`^(?:[-*_][ \t]*){3,}$`)
var validationSetext = regexp.MustCompile(`^(?:=+|-+)[ \t]*$`)

func validationTrim(value string) string {
	return strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
}

// ValidationReportBodyState checks structure, never the truth of a claim.
// Shared Go/TS cases deliberately allow short rationales without a length floor.
func ValidationReportBodyState(report string) string {
	report = validationComment.ReplaceAllString(report, "")
	report = strings.ReplaceAll(strings.ReplaceAll(report, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(report, "\n")
	title := false
	for i := 0; i < len(lines); i++ {
		line := validationTrim(validationQuote.ReplaceAllString(validationTrim(lines[i]), ""))
		if line == "" || validationFence.MatchString(line) || validationRule.MatchString(line) {
			continue
		}
		if validationHeading.MatchString(line) {
			if validationInlineSection.MatchString(line) {
				return "body_present"
			}
			title = true
			continue
		}
		if validationHTMLHeading.MatchString(line) {
			title = true
			continue
		}
		if i+1 < len(lines) && validationSetext.MatchString(validationTrim(lines[i+1])) {
			title = true
			i++
			continue
		}
		return "body_present"
	}
	if title {
		return "title_only"
	}
	return "empty"
}
