# Scan reliability: 2026-09-11

Addresses scan 13: empty child conclusions, sparse intermediate artifacts, repeated 429s, slow helper discovery, read-only /tmp, and unverified file progress.

- Custom-provider Chat and native Responses calls pass through a shared per-host request gate, keyed by upstream/model/credential hash. Default: two concurrent HTTP requests. SDK worker count remains its default. Cross-host coordination is not provided.
- The gateway owns 429 retries: three attempts maximum, shared cooldown, numeric/date Retry-After support. Delays over two minutes return the rate-limit error immediately; subsequent requests respect the cooldown. Quota exhaustion stops retries. No partially consumed stream is replayed automatically.
- Native empty/malformed/truncated streams fail explicitly instead of being reported as completed. Empty child final conclusions mark the resulting coverage partial; supervisor instructions require a conclusion or a documented fallback. This detects upstream failure; it cannot guarantee provider availability.
- Redacted assistant/tool evidence is appended synchronously under each attempt's checkpoints directory (10 MiB cap; each record 32 KiB). Valid canonical JSON drafts are snapshotted every 15 seconds. Saved records are unverified, downloadable evidence, not confirmed findings or resumable execution state.
- Host preflight invokes resolve_security_md.py with --scope and a 15-second timeout. Temporary files use the SDK artifact directory; TMPDIR/TMP/TEMP are restored after the attempt.
- File progress explicitly says scanner-reported and unverified. It is not used to imply verified coverage.

## Validation

107 scanner tests; 12 frontend tests; Go progress projection, checkpoint download (including symlink rejection), and quota classification tests; scanner/web/Go builds. A complete SDK mock scan succeeded with zero model tokens/cost. No paid scan was started or retried.

## Retry configuration reference

The custom provider configuration sets request_max_retries and stream_max_retries to zero because the gateway owns retry decisions. See the official [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference).

## Deployment

Deployment and backup receipts are retained privately on the original development host. API and agent restarted only after confirming no queued/running/preparing tasks. Existing task 13 remains failed; historical missing evidence cannot be reconstructed.

2026-10-16：已核实 SDK 0.1.25 没有跨进程续扫入口。Web 新增过期中断登记、完整提交结果恢复入库与明确确认后的重新执行，保持原执行、费用和历史证据。迁移 017 仅保存恢复来源；既有合同、SDK 锁与七处补丁不变。详情见 [扫描中断恢复](../SCAN_RECOVERY.md)。验证只使用本地 Mock / Fixture，业务 Docker 未升级。
