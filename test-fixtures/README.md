# M0 test fixtures

五个故意存在漏洞的最小样本，给 Provider Compatibility Gate 用。

**不要把这个目录当生产代码。** 每个子目录一个漏洞类型，预期 sink 写在各自的 `EXPECTED.md`。

| 目录 | 类型 | 语言 | 预期 CWE |
|---|---|---|---|
| `sql-injection` | SQL Injection | Python | CWE-89 |
| `command-injection` | Command Injection | Python | CWE-78 |
| `path-traversal` | Path Traversal | Python | CWE-22 |
| `ssrf` | SSRF | Python | CWE-918 |
| `hardcoded-secret` | Hardcoded Secret | Python | CWE-798 |

扫描入口：把 `test-fixtures/` 整棵树当 repository，或按子目录分别扫。
