package report_test

import (
	"axiom/contracts"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"axiom/code-audit-server/internal/report"
	"axiom/code-audit-server/internal/store"
)

func TestArchivedReportWithoutComparison(t *testing.T) {
	html, err := report.Build(&store.Task{ScanNo: "Fixtures / Mock", Attempt: 1, Status: "partial"}, []store.Finding{{Title: "historical evidence", Severity: "high"}}, nil, nil)
	if err != nil || !strings.Contains(html, "historical evidence") || !strings.Contains(html, "执行轮次</td><td>1</td>") || strings.Contains(html, "与上次扫描对比（") {
		t.Fatal("an archived report must render its evidence without a current comparison", err)
	}
}

func TestBuild(t *testing.T) {
	scan := &store.Task{
		ID:               1,
		ScanNo:           "SCAN-test",
		SourcePath:       "F:/data/snapshots/snap-abc",
		SourceSnapshotID: 3,
		Provider:         "deepseek",
		Model:            "deepseek-v4-flash-vision-exp",
		ScanMode:         "standard",
		Status:           "partial",
	}
	snap := &store.SnapshotRow{
		ID: 3, OriginalFilename: "repo.zip", ArchiveSha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContentHash: "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		FileCount:   4, TotalSize: 1024,
	}
	cmp := &report.Compare{
		PrevScanID: 1,
		Added:      []store.Finding{{Fingerprint: "fp-3", Title: "New SSRF", Severity: "medium", FilePath: "new.py"}},
		Unchanged:  1,
	}
	line := 8
	conf := 0.9
	findings := []store.Finding{
		{
			Fingerprint: "fp-1",
			Title:       `OS command injection & <script>alert(1)</script>`,
			Severity:    "high",
			CWE:         []string{"CWE-78"},
			FilePath:    "app.py",
			StartLine:   &line,
			Description: "os.system with user input",
			Evidence:    "host = request.args.get('host')",
			Remediation: "use subprocess args list",
			Confidence:  &conf,
			Status:      "false_positive",
		},
	}
	html, err := report.Build(scan, findings, snap, cmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"扫描报告",
		"OS command injection &amp; &lt;script&gt;",
		"CWE-78",
		"app.py</code>:8",
		"high",
		"快照 ID",
		"repo.zip",
		"01234567...89abcdef", // short() 前后各 8 位
		"false_positive",
		"与上次扫描对比",
		"New SSRF",
		"扫描 #1",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in report", want)
		}
	}
}

func TestComponentReportPreservesSelectedBatchScope(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/component-scan-request.json")
	if e != nil {
		t.Fatal(e)
	}
	var request contracts.ComponentScanRequest
	if e = json.Unmarshal(raw, &request); e != nil {
		t.Fatal(e)
	}
	request.SelectedIndices = []int64{1}
	request.PlanVersion.Document.Components[1].Name = "Web <script>"
	scan := &store.Task{ID: 1, ScanNo: "Fixtures / Mock components", Status: "partial", ComponentScan: &request}
	html, e := report.Build(scan, nil, nil, &report.Compare{})
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"本轮组件范围", "Web &lt;script&gt; · web", "未列出的发现不能视为已修复", "未知费用", string(request.PlanVersion.SHA256)} {
		if !strings.Contains(html, want) {
			t.Fatal("component scope lost", want)
		}
	}
	if strings.Contains(html, "API · api") {
		t.Fatal("unselected component was presented as scanned")
	}
}

func TestBuildWithoutCompareAndSnapshot(t *testing.T) {
	scan := &store.Task{ID: 1, ScanNo: "S", SourcePath: "F:/src", Status: "completed"}
	html, err := report.Build(scan, nil, nil, &report.Compare{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "与上次扫描对比") || strings.Contains(html, "扫描 #0") {
		t.Fatal("compare card must not render without previous scan")
	}
	if strings.Contains(html, "快照 ID") {
		t.Fatal("snapshot block must not render without snapshot")
	}
}
