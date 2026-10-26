package contracts

import (
	"encoding/json"
	"errors"
)

var ErrStructuredCatalogChanged = errors.New("diagnostic_catalog_changed")

func FixedStructuredDiagnosticCatalog() (StructuredDiagnosticCatalog, error) {
	return DecodeStructuredDiagnosticCatalog([]byte(structuredDiagnosticCatalogJSON), nil)
}

// Only fixed fixture metadata is returned. No SDK, credential, filesystem path,
// provider client or network operation belongs in this offline comparison.
func CheckStructuredDiagnosticReply(request StructuredDiagnosticRequestPreview, replay StructuredDiagnosticReplay) StructuredDiagnosticCheck {
	v := StructuredDiagnosticCheck{Profile: request.Profile, RequestSHA256: request.BodySHA256, FormatSHA256: request.FormatSHA256, ResponseSHA256: replay.ResponseSHA256, Transport: StructuredDiagnosticCheckTransport(replay.Transport), HttpStatus: replay.HttpStatus, BodyState: "not_evaluated"}
	fail := func(code string) StructuredDiagnosticCheck {
		e := StructuredDiagnosticCheckErrorCode(code)
		v.ErrorCode = &e
		return v
	}
	if replay.Transport == "http_error" {
		return fail("upstream_http_error")
	}
	if replay.Transport != "completed" {
		return fail("transport_incomplete")
	}
	var decoded any
	validJSON := CheckJSONText([]byte(replay.ResponseJSON)) == nil && json.Unmarshal([]byte(replay.ResponseJSON), &decoded) == nil
	v.JSONValid = &validJSON
	if !validJSON {
		return fail("response_json_invalid")
	}
	obj, ok := decoded.(map[string]any)
	disposition, _ := obj["disposition"].(string)
	report, reportIsString := obj["report"].(string)
	valid := ok && len(obj) == 2 && reportIsString && len(report) > 0 && (disposition == "reportable" || disposition == "suppressed" || disposition == "not_applicable" || disposition == "deferred")
	v.SchemaValid = &valid
	if !valid {
		return fail("response_schema_invalid")
	}
	d := StructuredDiagnosticCheckDisposition(disposition)
	v.Disposition = &d
	v.BodyState = StructuredDiagnosticCheckBodyState(ValidationReportBodyState(report))
	if v.BodyState != "body_present" {
		return fail("report_body_incomplete")
	}
	return v
}
func RunStructuredDiagnostic(request StructuredDiagnosticRequest, id, createdAt string) (StructuredDiagnosticRecord, error) {
	raw, e := EncodeJSON(request)
	if e != nil {
		return StructuredDiagnosticRecord{}, e
	}
	if e = ValidateJSON(KindStructuredDiagnosticRequest, raw, nil); e != nil {
		return StructuredDiagnosticRecord{}, e
	}
	if request.Protocol != "responses" {
		return StructuredDiagnosticRecord{}, errors.New("Chat diagnostics were removed; only Responses is supported")
	}
	if request.CatalogSHA256 != StructuredDiagnosticCatalogHash {
		return StructuredDiagnosticRecord{}, ErrStructuredCatalogChanged
	}
	catalog, e := FixedStructuredDiagnosticCatalog()
	if e != nil {
		return StructuredDiagnosticRecord{}, e
	}
	var replay StructuredDiagnosticReplay
	for _, v := range catalog.Scenarios {
		if v.ID == request.Scenario {
			replay = v
		}
	}
	result := StructuredDiagnosticRecord{SchemaVersion: 1, ID: id, CreatedAt: createdAt, SDKVersion: "0.5.0", Mode: "offline", CatalogSHA256: StructuredDiagnosticCatalogHash, SourceSHA256: catalog.Fixture.SourceSHA256, Protocol: request.Protocol, Scenario: request.Scenario, ProviderCompatibility: "not_tested", Model: "not_loaded", Results: []StructuredDiagnosticCheck{}}
	for _, p := range catalog.Requests {
		if p.Protocol == request.Protocol {
			result.Results = append(result.Results, CheckStructuredDiagnosticReply(p, replay))
		}
	}
	raw, e = EncodeJSON(result)
	if e != nil {
		return result, e
	}
	e = ValidateJSON(KindStructuredDiagnosticRecord, raw, nil)
	return result, e
}

// Messages are never interpolated into logs/errors. This helper is deliberately
// independent of model configuration and of business task IDs.
func StructuredDiagnosticRequestForRecord(v StructuredDiagnosticRecord) StructuredDiagnosticRequest {
	return StructuredDiagnosticRequest{SchemaVersion: 1, Mode: "offline", CatalogSHA256: v.CatalogSHA256, Protocol: v.Protocol, Scenario: v.Scenario}
}
