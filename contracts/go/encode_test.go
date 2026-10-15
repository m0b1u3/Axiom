package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvidenceTransportEncoding(t *testing.T) {
	text := strings.Repeat("<script>&中文</script>", 1000)
	raw, err := EncodeJSON(map[string]any{"evidence": text, "optional": Some(text), "raw": json.RawMessage(`{"code":"<a>&"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u003c`) || strings.Contains(string(raw), `\u0026`) {
		t.Fatal("source evidence was expanded into HTML escapes")
	}
	var value map[string]any
	if err = json.Unmarshal(raw, &value); err != nil || value["evidence"] != text {
		t.Fatal("evidence changed during encoding", err)
	}
}
