package scanner

import (
	"os"
	"testing"
)

func TestNativeConcurrencyDescriptionIsExplicit(t *testing.T) {
	for _, item := range []struct {
		file    string
		version int
		fail    bool
	}{
		{"description-sdk-concurrency", 1, false},
		{"description-sdk-parameters", 0, false},
		{"description-sdk-concurrency-missing", 0, true},
		{"description-sdk-concurrency-future", 0, true},
		{"description-sdk-concurrency-unverified-sdk", 0, true},
	} {
		t.Run(item.file, func(t *testing.T) {
			raw, e := os.ReadFile("../../../contracts/fixtures/v1/" + item.file + ".json")
			if e != nil {
				t.Fatal(e)
			}
			caps, e := parseDescription(raw)
			if (e != nil) != item.fail || (!item.fail && caps.ConcurrencyVersion != item.version) {
				t.Fatal(caps, e)
			}
		})
	}
}
