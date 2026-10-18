// Package evidence projects portable evidence facts. SDK field compatibility
// belongs to the Node adapter, never to this package or to the Agent.
package evidence

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Project(ctx context.Context, f contracts.Finding, ref contracts.ExecutionRef, sourceHash, archiveHash, sourceRoot string) (*contracts.EvidenceGraph, error) {
	facts, e := contracts.FindingEvidenceFor(f, ref, sourceHash)
	if e != nil {
		return nil, e
	}
	document, e := contracts.EncodeJSON(f)
	if e != nil {
		return nil, e
	}
	g := &contracts.EvidenceGraph{SchemaVersion: 1, Execution: ref, FindingFingerprint: f.Fingerprint, ProjectorVersion: 1, Origin: "native_evidence", Completeness: "complete", SourceFinding: document, Nodes: []contracts.EvidenceNode{}, Edges: []contracts.EvidenceRelation{}, Diagnostics: []contracts.EvidenceDiagnostic{}}
	if sourceHash != "" {
		hash := contracts.Digest(sourceHash)
		g.SourceContentHash = &hash
	}
	if archiveHash != "" {
		hash := contracts.Digest(archiveHash)
		g.ArchiveSHA256 = &hash
	}
	if facts != nil {
		g.Nodes = facts.Nodes
		g.Edges = facts.Edges
		g.Diagnostics = facts.Diagnostics
		g.Completeness = contracts.EvidenceGraphCompleteness(facts.Completeness)
	} else {
		legacy(g, f)
	}
	hasCode := false
	for _, n := range g.Nodes {
		if n.Kind == "code_evidence" && n.Code != nil && n.Location != nil && n.Location.StartLine != nil {
			hasCode = true
			break
		}
	}
	if hasCode {
		// An exact code match alone does not bind a file to the archived revision.
		// Verify the snapshot digest first; any failure keeps SDK claims inferred.
		check, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		hash, _, _, err := sourceidentity.TreeHash(check, sourceRoot)
		valid := err == nil && sourceHash != "" && hash == sourceHash
		if !valid {
			diagnostic(g, "snapshot_unavailable", "源码快照缺失、完整性不匹配或校验超时；代码证据保持推断状态。", nil)
		}
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if n.Kind != "code_evidence" || n.Code == nil || n.Location == nil || n.Location.StartLine == nil {
				continue
			}
			if !valid {
				n.SourceCheck = "unavailable"
				n.StateReason = "未能确认源码快照完整性；保留原始 SDK 代码描述。"
				continue
			}
			code, digest, err := sourceLines(sourceRoot, *n.Location, *n.Code)
			if err != nil {
				n.SourceCheck = "unavailable"
				n.StateReason = "指定源码位置暂不可读取；保留原始 SDK 代码描述。"
				diagnostic(g, "code_unavailable", n.StateReason, &n.SourcePointer)
				continue
			}
			n.SourceFileSHA256 = &digest
			if code {
				n.SourceCheck = "matched"
				n.State = "observed"
				n.StateReason = "指定片段与冻结源码逐行匹配；仅确认代码存在，不证明漏洞或数据流。"
			} else {
				n.SourceCheck = "mismatch"
				n.StateReason = "SDK 代码片段与冻结源码指定行不匹配；未确认该片段。"
				diagnostic(g, "code_mismatch", n.StateReason, &n.SourcePointer)
			}
		}
	}
	raw, e := contracts.EncodeJSON(g)
	if e != nil {
		return nil, e
	}
	if e = contracts.ValidateJSON(contracts.KindEvidenceGraph, raw, &ref); e != nil {
		return nil, e
	}
	return g, nil
}

func diagnostic(g *contracts.EvidenceGraph, code, message string, pointer *string) {
	if g.Completeness != "empty" {
		g.Completeness = "partial"
	}
	if len(g.Diagnostics) < 256 {
		g.Diagnostics = append(g.Diagnostics, contracts.EvidenceDiagnostic{Code: code, Message: message, SourcePointer: pointer})
	}
}

func sourceLines(rootPath string, location contracts.EvidenceLocation, code string) (bool, contracts.Digest, error) {
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return false, "", e
	}
	defer root.Close()
	// Reject directory symlinks as well as final symlinks, even if they remain
	// inside the root. Neither special files nor unbounded files are opened.
	parts := strings.Split(string(location.Path), "/")
	for i := range parts {
		info, e := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if e != nil {
			return false, "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, "", errors.New("symbolic link")
		}
		if i == len(parts)-1 && (!info.Mode().IsRegular() || info.Size() > 4<<20) {
			return false, "", errors.New("source file unavailable")
		}
	}
	file, e := root.Open(filepath.FromSlash(string(location.Path)))
	if e != nil {
		return false, "", e
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return false, "", errors.New("source file exceeds inspection limit")
	}
	hash := sha256.Sum256(raw)
	digest := contracts.Digest(hex.EncodeToString(hash[:]))
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	snippet := strings.TrimSuffix(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	count := len(strings.Split(snippet, "\n"))
	start := *location.StartLine - 1
	end := start + int64(count)
	if location.EndLine != nil {
		end = *location.EndLine
	}
	if start < 0 || end > int64(len(lines)) || end <= start {
		return false, digest, nil
	}
	return strings.Join(lines[start:end], "\n") == snippet, digest, nil
}

// Historical results keep their original objects. Only portable fields can
// form a legacy projection; Go never guesses SDK aliases or parses prose.
func legacy(g *contracts.EvidenceGraph, f contracts.Finding) {
	g.Origin = "legacy_projection"
	g.Completeness = "partial"
	diagnostic(g, "legacy_projection", "历史结果未记录统一证据引用；只展示已有字段，不补造支持关系或调用链。", nil)
	if f.Extensions.Present {
		if reason, ok := f.Extensions.Value["axiomEvidenceUnavailable"]; ok {
			var message string
			if json.Unmarshal(reason, &message) == nil && message != "" {
				diagnostic(g, "adapter_unavailable", message, nil)
			}
		}
	}
	add := func(kind contracts.EvidenceNodeKind, label, pointer string, location *contracts.EvidenceLocation, code *string) *contracts.EvidenceNode {
		if label == "" {
			return nil
		}
		if len(g.Nodes) >= 256 || len(label) > 16384 || (code != nil && len(*code) > 524288) {
			diagnostic(g, "projection_limit", "历史证据超过图谱展示容量；完整原文仍保留。", &pointer)
			return nil
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%s/%s/%s", g.Execution.TaskID, g.Execution.Attempt, f.Fingerprint, kind, pointer)))
		n := contracts.EvidenceNode{ID: hex.EncodeToString(hash[:]), Kind: kind, Label: label, State: "inferred", StateReason: "历史归一化结果中的分析内容；尚未独立确认。", SourcePointer: pointer, Location: location, Code: code, SourceCheck: "not_checked"}
		g.Nodes = append(g.Nodes, n)
		return &g.Nodes[len(g.Nodes)-1]
	}
	title := f.Title
	if title == "" || len(title) > 16384 {
		title = "历史漏洞（原标题见原始数据）"
		diagnostic(g, "legacy_title", "历史漏洞标题缺失或超过展示容量，原文仍保留。", nil)
	}
	root := add("finding", title, "/title", nil, nil)
	rootID := root.ID
	contains := func(n *contracts.EvidenceNode) {
		if n == nil {
			return
		}
		hash := sha256.Sum256([]byte(rootID + n.ID))
		g.Edges = append(g.Edges, contracts.EvidenceRelation{ID: hex.EncodeToString(hash[:]), From: rootID, To: n.ID, Kind: "contains", State: "inferred", Reason: "历史结果明确包含此条目；不推定其证明漏洞。", SourcePointer: n.SourcePointer})
	}
	for _, item := range []struct {
		kind    contracts.EvidenceNodeKind
		pointer string
		value   contracts.Optional[string]
	}{{"source", "/entrypoint", f.Entrypoint}, {"source", "/source", f.Source}, {"step", "/dataflow", f.Dataflow}, {"sink", "/sink", f.Sink}, {"impact", "/impact", f.Impact}, {"root_cause", "/rootCause", f.RootCause}, {"step", "/attackPath", f.AttackPath}, {"step", "/evidence", f.Evidence}} {
		if item.value.Present {
			contains(add(item.kind, item.value.Value, item.pointer, nil, nil))
		}
	}
	for i, c := range f.CodeEvidence.Value {
		var location *contracts.EvidenceLocation
		if c.Path.Present {
			location = &contracts.EvidenceLocation{Path: c.Path.Value}
			if c.StartLine.Present {
				x := c.StartLine.Value
				location.StartLine = &x
			}
			if c.EndLine.Present {
				x := c.EndLine.Value
				location.EndLine = &x
			}
		}
		label := "历史代码证据"
		if c.Label.Present && c.Label.Value != "" {
			label = c.Label.Value
		}
		code := c.Code
		var snippet *string
		if code != "" {
			snippet = &code
		}
		contains(add("code_evidence", label, fmt.Sprintf("/codeEvidence/%d", i), location, snippet))
	}
	for i, label := range f.AttackSteps.Value {
		contains(add("step", label, fmt.Sprintf("/attackSteps/%d", i), nil, nil))
	}
	if len(g.Nodes) >= 256 {
		diagnostic(g, "projection_limit", "历史图谱超过展示容量；完整结果仍可读取。", nil)
	}
	if len(g.Nodes) == 1 {
		g.Completeness = "empty"
	}
}
