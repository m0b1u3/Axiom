# Chat protocol adapter reliability

Reviewed against cc-switch official `main` on 2026-09-10:
https://github.com/farion1231/cc-switch/blob/main/src-tauri/src/proxy/providers/streaming_codex_chat.rs

The Axiom adapter translates Codex Responses requests to Chat Completions. This revision adapts cc-switch's explicit finish-state checks, late tool identity handling, terminal error propagation and EOF handling to the scanner's TypeScript adapter. It is not a full port of cc-switch's desktop tool/plugin ecosystem.

## Behavior

- Require a supported `finish_reason`. `[DONE]` alone or transport EOF does not prove the model completed its work.
- Reject empty/reasoning-only answers, filtered/truncated responses, malformed SSE and incomplete tool calls. No successful terminal event or executable tool completion is emitted for these turns.
- Unlike cc-switch's `response.completed` with `status=incomplete`, Axiom emits `response.failed` for truncation. A security scan must not silently advance to artifact validation with an unfinished discovery turn.
- Preserve late IDs/names, empty continuation fields, parallel tool order and accumulated JSON arguments. Reject ambiguous fragments instead of guessing.
- Parse LF, CRLF, CR, multiline data, comments, UTF-8 chunks and final un-delimited frames. A valid finish reason may complete without `[DONE]`; record that distinction.
- Convert JSON responses through the same checked state machine when the upstream ignores streaming. Forward validation `text.format` JSON Schema as Chat `response_format`.
- Preserve usage even when attached to a choice; include reasoning token counts. Reasoning text is not promoted to an answer or logged. Existing request-side reasoning policy remains unchanged.
- Journal diagnostics contain generated response ID, finish reason, counts, duration, upstream HTTP status and error code. No prompts, source text, tool arguments or credentials are stored by these diagnostics.

## Verification / operation

Use `npm test` and `npm run build` in `code-audit-scanner`. Protocol tests use an in-process HTTP upstream; they do not call providers or create scans. Deploy the source and built adapter together. Agent spawns a fresh scanner per attempt; existing attempts retain their loaded code. Do not automatically retry historical failed scans.

This fixes confirmed adapter defects; it cannot reconstruct the missing upstream response for task 11 or prove which defect caused that historical failure.
