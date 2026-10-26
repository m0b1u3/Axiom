package contracts

import (
	"encoding/json"
	"os"
	"testing"
)

func TestValidationReportBodySharedCases(t *testing.T) {
	raw, err := os.ReadFile("../fixtures/validation-report-body.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Report, State string }
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := ValidationReportBodyState(c.Report); got != c.State {
				t.Fatalf("got %s, want %s", got, c.State)
			}
		})
	}
}
