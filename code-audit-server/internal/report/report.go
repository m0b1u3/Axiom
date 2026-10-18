package report

import (
	"bytes"
	"html/template"
	"strings"
	"time"

	"axiom/code-audit-server/internal/store"
)

type SeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

type Compare struct {
	PrevScanID int64
	Added      []store.Finding
	Removed    []store.Finding
	Unchanged  int
}

type Model struct {
	ComponentScope []string
	Scan           *store.Task
	Snapshot       *store.SnapshotRow
	Compare        *Compare
	Findings       []store.Finding
	Counts         SeverityCounts
	GeneratedAt    string
}

func Build(scan *store.Task, findings []store.Finding, snap *store.SnapshotRow, cmp *Compare) (string, error) {
	if cmp == nil {
		cmp = &Compare{}
	}
	m := Model{
		Scan:        scan,
		Snapshot:    snap,
		Compare:     cmp,
		Findings:    findings,
		Counts:      tally(findings),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if scan.ComponentScan != nil {
		for _, i := range scan.ComponentScan.SelectedIndices {
			c := scan.ComponentScan.PlanVersion.Document.Components[i]
			paths := []string{}
			for _, p := range c.Paths {
				paths = append(paths, string(p))
			}
			m.ComponentScope = append(m.ComponentScope, c.Name+" · "+strings.Join(paths, ", "))
		}
	}
	var buf bytes.Buffer
	if err := templateText.Execute(&buf, m); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func tally(findings []store.Finding) SeverityCounts {
	var c SeverityCounts
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			c.Critical++
		case "high":
			c.High++
		case "medium":
			c.Medium++
		case "low":
			c.Low++
		default:
			c.Info++
		}
	}
	return c
}

var templateText = template.Must(template.New("report").Funcs(template.FuncMap{
	"short": func(s string) string {
		if len(s) > 16 {
			return s[:8] + "..." + s[len(s)-8:]
		}
		return s
	},
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>扫描报告 {{.Scan.ScanNo}}</title>
<style>
body { font-family: "Helvetica Neue", "PingFang SC", "Microsoft YaHei", sans-serif; margin: 0; background: #f5f7fa; color: #303133; }
.shell { max-width: 960px; margin: 0 auto; padding: 24px; }
h1 { margin: 0 0 4px; font-size: 22px; }
.meta { color: #909399; font-size: 14px; margin-bottom: 16px; }
.card { background: #fff; border: 1px solid #e4e7ed; border-radius: 6px; padding: 16px 20px; margin-bottom: 16px; }
.card h3 { margin: 0 0 10px; }
.counts { display: flex; gap: 24px; }
.count { text-align: center; }
.count b { display: block; font-size: 24px; }
.kv td { padding: 4px 16px 4px 0; font-size: 14px; }
.kv td:first-child { color: #909399; }
table.f { width: 100%; border-collapse: collapse; }
table.f th, table.f td { border-bottom: 1px solid #ebeef5; padding: 8px; text-align: left; font-size: 14px; vertical-align: top; }
.badge { display: inline-block; padding: 1px 8px; border-radius: 10px; color: #fff; font-size: 12px; }
.badge.critical, .badge.high { background: #f56c6c; }
.badge.medium { background: #e6a23c; }
.badge.low, .badge.info { background: #909399; }
.status.open { color: #909399; }
.status.confirmed { color: #e6a23c; font-weight: 600; }
.status.false_positive { color: #909399; text-decoration: line-through; }
.status.fixed { color: #67c23a; }
.detail { font-size: 13px; color: #606266; }
.detail .dl { color: #909399; }
code { background: #f5f7fa; padding: 1px 4px; border-radius: 3px; }
.compare-list { margin-top: 6px; }
.compare-item { font-size: 13px; line-height: 24px; }
.compare-item.removed { color: #909399; }
</style>
</head>
<body>
<div class="shell">
<h1>{{.Scan.ScanNo}} — 安全扫描报告</h1>
<div class="meta">生成时间：{{.GeneratedAt}}</div>

<div class="card">
  <table class="kv">
    <tr><td>代码版本</td><td>{{.Scan.SourcePath}}</td></tr>
    {{if .Scan.SourceSnapshotID}}<tr><td>快照 ID</td><td>#{{.Scan.SourceSnapshotID}}</td></tr>
    {{if .Snapshot}}<tr><td>原文件名</td><td>{{.Snapshot.OriginalFilename}}</td></tr>
    <tr><td>文件数/总大小</td><td>{{.Snapshot.FileCount}} / {{.Snapshot.TotalSize}} B</td></tr>
    <tr><td>归档 SHA256</td><td><code>{{short .Snapshot.ArchiveSha256}}</code></td></tr>
    <tr><td>内容哈希</td><td><code>{{short .Snapshot.ContentHash}}</code></td></tr>{{end}}{{end}}
    <tr><td>引擎</td><td>codex-security</td></tr>
    <tr><td>模型</td><td>{{.Scan.Provider}} / {{.Scan.Model}}</td></tr>
    <tr><td>扫描模式</td><td>{{.Scan.ScanMode}}</td></tr>
    <tr><td>执行轮次</td><td>{{.Scan.Attempt}}</td></tr>
    {{if .Scan.ComponentScan}}<tr><td>组件计划 / 版本</td><td>#{{.Scan.ComponentScan.PlanVersion.PlanID}} / v{{.Scan.ComponentScan.PlanVersion.Version}}</td></tr><tr><td>冻结版本 SHA-256</td><td><code>{{.Scan.ComponentScan.PlanVersion.SHA256}}</code></td></tr><tr><td>本轮组件范围</td><td>{{range .ComponentScope}}<div>{{.}}</div>{{end}}</td></tr><tr><td>范围与费用说明</td><td>此报告只包含本次执行选中的组件。历史已完成组件保留在原执行中；未列出的发现不能视为已修复，不能推定全历史漏洞已去重。原生跨组件匹配可能产生未知费用，不受组件扫描预算上限限制。</td></tr>{{end}}
    <tr><td>任务状态</td><td>{{.Scan.Status}}</td></tr>
    <tr><td>创建时间</td><td>{{.Scan.CreatedAt}}</td></tr>
    <tr><td>开始时间</td><td>{{.Scan.StartedAt}}</td></tr>
    <tr><td>完成时间</td><td>{{.Scan.FinishedAt}}</td></tr>
    {{if .Scan.ErrorMessage}}<tr><td>错误</td><td>{{.Scan.ErrorCode}}: {{.Scan.ErrorMessage}}</td></tr>{{end}}
  </table>
</div>

<div class="card">
  <h3>漏洞统计</h3>
  <div class="counts">
    <div class="count"><b>{{.Counts.Critical}}</b>Critical</div>
    <div class="count"><b>{{.Counts.High}}</b>High</div>
    <div class="count"><b>{{.Counts.Medium}}</b>Medium</div>
    <div class="count"><b>{{.Counts.Low}}</b>Low</div>
    <div class="count"><b>{{.Counts.Info}}</b>Info</div>
    <div class="count"><b>{{len .Findings}}</b>合计</div>
  </div>
</div>

{{if .Compare.PrevScanID}}
<div class="card">
  <h3>与上次扫描对比（扫描 #{{.Compare.PrevScanID}}）</h3>
  <div class="counts">
    <div class="count"><b>{{len .Compare.Added}}</b>新增</div>
    <div class="count"><b>{{len .Compare.Removed}}</b>不再出现</div>
    <div class="count"><b>{{.Compare.Unchanged}}</b>保持</div>
  </div>
  {{if .Compare.Added}}
  <div class="compare-list">
    {{range .Compare.Added}}<div class="compare-item"><b>{{.Severity}}</b> {{.Title}} <span class="meta">{{.FilePath}}</span></div>{{end}}
  </div>
  {{end}}
  {{if .Compare.Removed}}
  <div class="compare-list">
    {{range .Compare.Removed}}<div class="compare-item removed">{{.Severity}} {{.Title}} <span class="meta">{{.FilePath}}</span></div>{{end}}
  </div>
  {{end}}
</div>
{{end}}

<div class="card">
  <h3>漏洞明细</h3>
  {{if .Findings}}
  <table class="f">
    <thead>
      <tr><th style="width:80px">级别</th><th style="width:220px">位置</th><th>标题</th><th style="width:120px">状态</th></tr>
    </thead>
    <tbody>
      {{range .Findings}}
      <tr>
        <td><span class="badge {{.Severity}}">{{.Severity}}</span></td>
        <td>{{if .FilePath}}<code>{{.FilePath}}</code>{{if .StartLine}}:{{.StartLine}}{{end}}{{else}}-{{end}}</td>
        <td>
          <div class="title">{{.Title}}</div>
          {{if .CWE}}<div class="detail"><span class="dl">CWE:</span> {{range .CWE}}{{.}} {{end}}</div>{{end}}
          {{if .Description}}<div class="detail">{{.Description}}</div>{{end}}
          {{if .Evidence}}<div class="detail"><span class="dl">证据:</span> {{.Evidence}}</div>{{end}}
          {{if .Remediation}}<div class="detail"><span class="dl">修复建议:</span> {{.Remediation}}</div>{{end}}
        </td>
        <td><span class="status {{.Status}}">{{.Status}}</span></td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}
  <div>本次扫描没有发现漏洞。</div>
  {{end}}
</div>

<div class="meta">Generated by Axiom code audit appliance.</div>
</div>
</body>
</html>
`))
