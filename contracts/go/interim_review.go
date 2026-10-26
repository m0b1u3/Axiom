package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func InterimReviewDigest(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }

// Preserve exact stored bytes. No cross-language JSON reserialization is used
// for either the frozen input hash or original evidence artifact hashes.
func ReadInterimReview(envelope InterimReviewEnvelope, source string) (InterimReviewInput, error) {
	var zero InterimReviewInput
	if InterimReviewDigest([]byte(envelope.InputJSON)) != string(envelope.InputSHA256) {
		return zero, fmt.Errorf("frozen review input digest mismatch")
	}
	v, e := DecodeInterimReviewInput([]byte(envelope.InputJSON), nil)
	if e != nil {
		return v, e
	}
	if string(v.SourceContentHash) != source || len(v.Members) == 0 {
		return v, fmt.Errorf("review source mismatch")
	}
	docs := map[string]InterimDocument{}
	for _, item := range v.Documents {
		a := item.Artifact
		key := string(item.Execution.TaskID) + fmt.Sprint(":", item.Execution.Attempt, ":") + string(a.SHA256)
		if _, ok := docs[key]; ok {
			return v, fmt.Errorf("duplicate review document")
		}
		raw := []byte(item.DocumentJSON)
		if a.Kind != "engine_raw" || string(a.RelativePath) != "checkpoints/interim/objects/"+string(a.SHA256)+".json" || int64(len(raw)) != int64(a.SizeBytes) || InterimReviewDigest(raw) != string(a.SHA256) {
			return v, fmt.Errorf("review artifact integrity mismatch")
		}
		d, err := DecodeInterimDocument(raw, &item.Execution)
		if err != nil || string(d.SourceContentHash) != source {
			return v, fmt.Errorf("review evidence identity mismatch")
		}
		docs[key] = d
	}
	seen := map[string]bool{}
	for _, m := range v.Members {
		key := string(m.Execution.TaskID) + fmt.Sprint(":", m.Execution.Attempt, ":") + string(m.Candidate.Artifact)
		d, ok := docs[key]
		identity := key + ":" + string(m.Candidate.ID)
		if !ok || seen[identity] || InterimReviewDigest([]byte(string(d.SourcePath)+"\x00"+m.Candidate.Pointer)) != string(m.Candidate.ID) {
			return v, fmt.Errorf("review candidate provenance mismatch")
		}
		seen[identity] = true
	}
	if v.Grouping == "manual" && len(v.GroupingReason) == 0 {
		return v, fmt.Errorf("manual grouping requires rationale")
	}
	return v, nil
}
