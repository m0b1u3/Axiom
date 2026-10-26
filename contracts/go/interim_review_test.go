package contracts

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFrozenInterimReviewIntegrity(t *testing.T) {
	raw, e := os.ReadFile("../fixtures/v1/interim-review-envelope.json")
	if e != nil {
		t.Fatal(e)
	}
	envelope, e := DecodeInterimReviewEnvelope(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	input, e := ReadInterimReview(envelope, ""+string(makeDigestA()))
	if e != nil {
		t.Fatal(e)
	}
	if len(input.Members) != 1 {
		t.Fatal(input)
	}
	for _, mutate := range []func(*InterimReviewInput){func(v *InterimReviewInput) { v.Documents[0].DocumentJSON += " " }, func(v *InterimReviewInput) {
		v.Members[0].Candidate.ID = Digest("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	}, func(v *InterimReviewInput) { v.Members = append(v.Members, v.Members[0]) }} {
		var v InterimReviewInput
		json.Unmarshal([]byte(envelope.InputJSON), &v)
		mutate(&v)
		raw, _ := EncodeJSON(v)
		bad := InterimReviewEnvelope{SchemaVersion: 1, InputJSON: string(raw), InputSHA256: Digest(InterimReviewDigest(raw))}
		if _, err := ReadInterimReview(bad, string(makeDigestA())); err == nil {
			t.Fatal("tamper accepted")
		}
	}
}
func makeDigestA() Digest {
	return Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
}
