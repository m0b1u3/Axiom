package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidationError contains no input values or credential material.
type ValidationError struct {
	Code string
	Kind Kind
}

func (e *ValidationError) Error() string   { return fmt.Sprintf("%s: %s", e.Kind, e.Code) }
func invalid(code string, kind Kind) error { return &ValidationError{Code: code, Kind: kind} }

type offlineLoader struct{}

func (offlineLoader) Load(_ string) (any, error) {
	return nil, errors.New("contract resources must be embedded")
}

var compileOnce sync.Once
var compiled map[Kind]*jsonschema.Schema
var compileErr error

// CheckJSONText rejects encodings that Go would silently replace or round into
// a different document before API-to-contract mapping.
func CheckJSONText(data []byte) error {
	if !utf8.Valid(data) || !json.Valid(data) || !validJSONStrings(data) {
		return errors.New("invalid JSON encoding")
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("invalid JSON number")
	}
	return nil
}

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(offlineLoader{})
	compiler.AssertFormat()
	for id, text := range schemaResources {
		var resource any
		if err := json.Unmarshal([]byte(text), &resource); err != nil {
			compileErr = err
			return
		}
		if err := compiler.AddResource(id, resource); err != nil {
			compileErr = err
			return
		}
	}
	compiled = make(map[Kind]*jsonschema.Schema)
	for kind, id := range schemaIDs {
		schema, err := compiler.Compile(id)
		if err != nil {
			compileErr = err
			return
		}
		compiled[kind] = schema
	}
}

func limitFor(kind Kind) int {
	switch kind {
	case KindProgressSnapshot, KindProgressSnapshotV2, KindExecutionHeartbeat, KindSDKDiagnostics:
		return MaxProgressBytes
	case KindScanEvent, KindScanEventV2, KindSDKDiagnostic:
		return MaxEventBytes
	default:
		return MaxDocumentBytes
	}
}

// ValidateJSON checks syntax, size, schema, and optionally the expected execution.
// It neither normalizes defaults nor mutates the document or any production state.
func ValidateJSON(kind Kind, data []byte, expected *ExecutionRef) error {
	if _, ok := schemaIDs[kind]; !ok {
		return invalid("unsupported_kind", kind)
	}
	if len(data) > limitFor(kind) {
		return invalid("payload_too_large", kind)
	}
	if !utf8.Valid(data) {
		return invalid("invalid_json", kind)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&value); err != nil {
		return invalid("invalid_json", kind)
	}
	if decoder.Decode(new(any)) == nil {
		return invalid("invalid_json", kind)
	}
	// json.Valid also rejects trailing non-JSON bytes after a complete document.
	if !json.Valid(data) {
		return invalid("invalid_json", kind)
	}
	if !validJSONStrings(data) {
		return invalid("invalid_json", kind)
	}
	compileOnce.Do(compileSchemas)
	if compileErr != nil {
		return fmt.Errorf("contract schema compilation failed: %w", compileErr)
	}
	if err := compiled[kind].Validate(value); err != nil {
		return invalid("invalid_contract", kind)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return invalid("invalid_contract", kind)
	}
	execution, ok := object["execution"].(map[string]any)
	if kind == KindExecutionRef {
		execution = object
		ok = true
	}
	if expected != nil && (!ok || execution["taskId"] != string(expected.TaskID) || execution["attempt"] != float64(expected.Attempt)) {
		return invalid("identity_mismatch", kind)
	}
	if kind == KindFindingEvidence || kind == KindEvidenceGraph {
		if err := validateEvidenceFields(kind, object); err != nil {
			return err
		}
	}
	if kind == KindScanRecoverySnapshot {
		if recovery, ok := object["recovery"].(map[string]any); ok {
			if cost, ok := recovery["previousCost"].(map[string]any); ok {
				nested := cost["execution"].(map[string]any)
				if nested["taskId"] != execution["taskId"] || nested["attempt"] != execution["attempt"] {
					return invalid("identity_mismatch", kind)
				}
			}
			if previous, ok := recovery["previousCompletion"].(map[string]any); ok {
				raw, _ := json.Marshal(previous)
				ref := &ExecutionRef{TaskID: TaskID(execution["taskId"].(string)), Attempt: int64(execution["attempt"].(float64))}
				if e := ValidateJSON(KindExecutionCompletion, raw, ref); e != nil {
					return e
				}
			}
		}
	}
	if kind == KindExecutionCompletion {
		if result, exists := object["result"].(map[string]any); exists {
			nested := result["execution"].(map[string]any)
			if nested["taskId"] != execution["taskId"] || nested["attempt"] != execution["attempt"] {
				return invalid("identity_mismatch", kind)
			}
		}
		_, hasResult := object["result"]
		process := object["process"].(map[string]any)
		if process["resultCommitted"] != hasResult {
			return invalid("invalid_contract", kind)
		}
	}
	if e := validateIdentityFields(kind, object); e != nil {
		return e
	}
	if err := validateComponentScanFields(kind, object, execution); err != nil {
		return err
	}
	if err := validateComponentFields(kind, object, execution); err != nil {
		return err
	}
	if err := validateSDKOperationFields(kind, object, execution); err != nil {
		return err
	}
	if kind == KindSDKNativeResult || kind == KindSDKDiagnostics {
		expectedNested := &ExecutionRef{TaskID: TaskID(execution["taskId"].(string)), Attempt: int64(execution["attempt"].(float64))}
		if kind == KindSDKNativeResult {
			for key, nestedKind := range map[string]Kind{"usageEstimate": KindSDKCostEstimate, "diagnostics": KindSDKDiagnostics} {
				raw, _ := json.Marshal(object[key])
				if err := ValidateJSON(nestedKind, raw, expectedNested); err != nil {
					return err
				}
			}
		} else {
			warnings, observers := object["warningCount"].(float64), object["observerErrorCount"].(float64)
			total, omitted := warnings+observers, object["omittedCount"].(float64)
			entries := object["entries"].([]any)
			if total > 9007199254740991 || omitted+float64(len(entries)) != total {
				return invalid("invalid_contract", kind)
			}
			var retainedWarnings, retainedObservers float64
			for index, value := range entries {
				entry := value.(map[string]any)
				raw, _ := json.Marshal(entry)
				if err := ValidateJSON(KindSDKDiagnostic, raw, expectedNested); err != nil {
					return err
				}
				if entry["sequence"] != omitted+float64(index)+1 {
					return invalid("invalid_contract", kind)
				}
				if entry["source"] == "sdk_warning" {
					retainedWarnings++
				} else {
					retainedObservers++
				}
			}
			if retainedWarnings > warnings || retainedObservers > observers {
				return invalid("invalid_contract", kind)
			}
		}
	}
	if kind == KindExecutionHeartbeat {
		if progress, exists := object["progress"].(map[string]any); exists {
			nested := progress["execution"].(map[string]any)
			if nested["taskId"] != execution["taskId"] || nested["attempt"] != execution["attempt"] {
				return invalid("identity_mismatch", kind)
			}
		}
	}

	if kind == KindExecutionAssignment {
		if credential, exists := object["credential"].(map[string]any); exists {
			model := object["model"].(map[string]any)
			if credential["envName"] != model["credentialEnv"] {
				return invalid("credential_binding_mismatch", kind)
			}
		}
	}
	return nil
}

func NormalizeLegacyErrorCode(code string) (ErrorCode, bool) {
	if value, ok := legacyErrorAliases[code]; ok {
		return value, true
	}
	value := ErrorCode(code)
	if canonicalErrorCodes[value] {
		return value, true
	}
	return ErrorCodeUnknown, false
}
func ExitCode(status TerminalStatus) (int, bool) {
	value, ok := terminalExitCodes[status]
	return value, ok
}

// JSON Schema integers include JSON numbers such as 2.0 and 2e0. Normalize
// number spelling before decoding into Go integer fields, just as JSON.parse does.
func normalizedJSON(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return EncodeJSON(value)
}
