package contracts

import (
	"bytes"
	"encoding/json"
)

// EncodeJSON is the portable transport encoding, not an HTML script literal.
// Avoid expanding source evidence containing <, > or & before byte-limit checks.
func EncodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}
