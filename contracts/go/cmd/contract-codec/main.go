// contract-codec is a JSONL conformance-test process, not a production transport.
package main

import (
	"axiom/contracts"
	"bufio"
	"encoding/json"
	"errors"
	"os"
)

type request struct {
	Kind     contracts.Kind          "json:\"kind\""
	JSON     string                  "json:\"json\""
	Expected *contracts.ExecutionRef "json:\"expectedExecution,omitempty\""
}
type response struct {
	Valid    bool            "json:\"valid\""
	Code     string          "json:\"code,omitempty\""
	Document json.RawMessage "json:\"document,omitempty\""
}

func main() {
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 64*1024), contracts.MaxDocumentBytes*2)
	encoder := json.NewEncoder(os.Stdout)
	for reader.Scan() {
		var input request
		if err := json.Unmarshal(reader.Bytes(), &input); err != nil {
			encoder.Encode(response{Code: "invalid_request"})
			continue
		}
		if input.Kind == "StructuredDiagnosticOfflineTest" {
			request, err := contracts.DecodeStructuredDiagnosticRequest([]byte(input.JSON), nil)
			if err != nil {
				encoder.Encode(response{Code: "invalid_contract"})
				continue
			}
			record, err := contracts.RunStructuredDiagnostic(request, "11111111111111111111111111111111", "2026-10-22T08:00:00.000Z")
			if err != nil {
				encoder.Encode(response{Code: "invalid_contract"})
				continue
			}
			data, err := contracts.EncodeJSON(record)
			if err != nil {
				os.Exit(2)
			}
			encoder.Encode(response{Valid: true, Document: data})
			continue
		}
		data, err := contracts.RoundTrip(input.Kind, []byte(input.JSON), input.Expected)
		if err != nil {
			var validation *contracts.ValidationError
			if !errors.As(err, &validation) {
				os.Stderr.WriteString(err.Error() + "\n")
				os.Exit(2)
			}
			encoder.Encode(response{Code: validation.Code})
			continue
		}
		encoder.Encode(response{Valid: true, Document: data})
	}
	if reader.Err() != nil {
		os.Exit(2)
	}
}
