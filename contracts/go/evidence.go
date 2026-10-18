package contracts

import (
	"encoding/json"
	"strconv"
	"strings"
)

// EvidencePointer follows only JSON members and canonical array indices.
func EvidencePointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for _, part := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				i++
				if i >= len(part) || (part[i] != '0' && part[i] != '1') {
					return nil, false
				}
			}
		}
		key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch current := value.(type) {
		case map[string]any:
			var exists bool
			value, exists = current[key]
			if !exists {
				return nil, false
			}
		case []any:
			index, e := strconv.Atoi(key)
			if e != nil || index < 0 || strconv.Itoa(index) != key || index >= len(current) {
				return nil, false
			}
			value = current[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func validateEvidenceFields(kind Kind, object map[string]any) error {
	nodes := object["nodes"].([]any)
	edges := object["edges"].([]any)
	ids := map[string]bool{}
	relations := map[string]bool{}
	for _, raw := range nodes {
		n := raw.(map[string]any)
		id := n["id"].(string)
		if ids[id] {
			return invalid("invalid_contract", kind)
		}
		ids[id] = true
		if loc, ok := n["location"].(map[string]any); ok {
			if end, ok := loc["endLine"].(float64); ok {
				start, ok := loc["startLine"].(float64)
				if !ok || end < start {
					return invalid("invalid_contract", kind)
				}
			}
			if n["state"] == "observed" && loc["startLine"] == nil {
				return invalid("invalid_contract", kind)
			}
		}
	}
	for _, raw := range edges {
		e := raw.(map[string]any)
		id := e["id"].(string)
		if relations[id] || !ids[e["from"].(string)] || !ids[e["to"].(string)] {
			return invalid("invalid_contract", kind)
		}
		relations[id] = true
	}
	if kind == KindEvidenceGraph {
		source, ok := object["sourceFinding"].(map[string]any)
		if !ok || source["fingerprint"] != object["findingFingerprint"] {
			return invalid("identity_mismatch", kind)
		}
		for _, raw := range append(nodes, edges...) {
			if _, ok := EvidencePointer(source, raw.(map[string]any)["sourcePointer"].(string)); !ok {
				return invalid("invalid_contract", kind)
			}
		}
		for _, raw := range nodes {
			node := raw.(map[string]any)
			origin, _ := EvidencePointer(source, node["sourcePointer"].(string))
			if !evidenceCodeBound(node, origin) {
				return invalid("invalid_contract", kind)
			}
		}
		for _, raw := range edges {
			edge := raw.(map[string]any)
			if edge["kind"] != "supports" {
				continue
			}
			for _, item := range nodes {
				n := item.(map[string]any)
				if n["id"] == edge["from"] {
					origin, _ := EvidencePointer(source, edge["sourcePointer"].(string))
					if n["sdkEvidenceId"] == nil || n["sdkEvidenceId"] != origin {
						return invalid("invalid_contract", kind)
					}
				}
			}
		}
	}
	return nil
}

// SDK-specific names never cross this boundary. Invalid optional evidence is
// reported separately; the original result remains readable.
func FindingEvidenceFor(finding Finding, execution ExecutionRef, sourceHash string) (*FindingEvidence, error) {
	raw, exists := finding.Extensions.Value["axiomEvidence"]
	if !exists {
		return nil, nil
	}
	v, e := DecodeFindingEvidence(raw, &execution)
	if e != nil {
		return nil, e
	}
	if v.FindingFingerprint != finding.Fingerprint || (v.SourceContentHash == nil && sourceHash != "") || (v.SourceContentHash != nil && string(*v.SourceContentHash) != sourceHash) {
		return nil, invalid("identity_mismatch", KindFindingEvidence)
	}
	document, _ := EncodeJSON(finding)
	var source any
	if json.Unmarshal(document, &source) != nil {
		return nil, invalid("invalid_contract", KindFindingEvidence)
	}
	for _, n := range v.Nodes {
		origin, ok := EvidencePointer(source, n.SourcePointer)
		nodeRaw, _ := EncodeJSON(n)
		var node map[string]any
		json.Unmarshal(nodeRaw, &node)
		if !ok || !evidenceCodeBound(node, origin) {
			return nil, invalid("invalid_contract", KindFindingEvidence)
		}
	}
	for _, r := range v.Edges {
		origin, ok := EvidencePointer(source, r.SourcePointer)
		if !ok {
			return nil, invalid("invalid_contract", KindFindingEvidence)
		}
		if r.Kind == "supports" {
			for _, n := range v.Nodes {
				if n.ID == r.From && (n.SDKEvidenceID == nil || origin != *n.SDKEvidenceID) {
					return nil, invalid("invalid_contract", KindFindingEvidence)
				}
			}
		}
	}
	return &v, nil
}

func evidenceCodeBound(node map[string]any, origin any) bool {
	if node["kind"] != "code_evidence" {
		return true
	}
	source, ok := origin.(map[string]any)
	if !ok {
		return false
	}
	var code any
	if text, ok := source["code"].(string); ok && text != "" {
		code = text
	}
	if node["code"] != code {
		return false
	}
	if node["sdkEvidenceId"] != nil && source["id"] != node["sdkEvidenceId"] {
		return false
	}
	if loc, ok := node["location"].(map[string]any); ok {
		if source["path"] != loc["path"] || source["startLine"] != loc["startLine"] || source["endLine"] != loc["endLine"] {
			return false
		}
	}
	return true
}
