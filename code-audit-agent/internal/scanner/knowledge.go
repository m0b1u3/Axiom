package scanner

import (
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// The directory belongs to one execution attempt. Paths and names are derived
// here, never accepted from a Web client or an SDK prompt.
func materializeKnowledge(output string, input contracts.ScanInput) error {
	raw, e := contracts.EncodeJSON(input)
	if e != nil {
		return e
	}
	if _, e = contracts.DecodeScanInput(raw, nil); e != nil {
		return e
	}
	if len(input.Knowledge) == 0 {
		return nil
	}
	directory := filepath.Join(output, "runtime", "knowledge")
	if e := os.MkdirAll(filepath.Dir(directory), 0700); e != nil {
		return e
	}
	if e := os.Mkdir(directory, 0700); e != nil {
		return errors.New("knowledge materialization directory already exists or is unavailable")
	}
	for _, d := range input.Knowledge {
		sum := sha256.Sum256([]byte(d.Content))
		if hex.EncodeToString(sum[:]) != d.SHA256 {
			return errors.New("knowledge content hash mismatch")
		}
		if len(d.Content) > contracts.KnowledgeDocumentMaxBytes {
			return errors.New("knowledge document exceeds byte limit")
		}
		name := filepath.Join(directory, d.VersionID+".md")
		f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(d.Content)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
