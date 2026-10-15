/**
 * AXIOM PATCH installer — 幂等。
 *
 * 背景：@openai/codex-security runtime.js 的 executablePathForSpawn 对 Windows
 * 绝对路径调用 toNamespacedPath 加 `\\?\` 前缀；Node 22 上 child_process.spawn
 * 带该前缀必报 ENOENT，导致所有真实扫描冷启动失败（os error 2）。
 * 补丁：spawn 直接用普通绝对路径。
 *
 * 用法：node deploy/patch-codex-runtime.mjs
 * 可挂 package.json postinstall；重复执行安全（检测 marker 后跳过）。
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import * as url from "node:url";
import * as path from "node:path";

const here = path.dirname(url.fileURLToPath(import.meta.url));
const file = path.join(here, "..", "code-audit-scanner", "node_modules", "@openai", "codex-security", "dist", "runtime.js");

if (!existsSync(file)) {
  console.log("[patch] runtime.js not found (deps not installed yet):", file);
  process.exit(0);
}

const MARKER = "AXIOM PATCH";
let src = readFileSync(file, "utf8");
if (src.includes(MARKER)) {
  console.log("[patch] already applied — skip");
  process.exit(0);
}

const old =
  `    const root = win32.parse(command).root;\n` +
  `    return root === "\\\\" || root === "/"\n` +
  `        ? command\n` +
  `        : win32.toNamespacedPath(command);`;

const next =
  `    const root = win32.parse(command).root;\n` +
  `    // AXIOM PATCH: toNamespacedPath (\`\\\\\\\\?\\\\\` prefix) makes\n` +
  `    // child_process.spawn fail with ENOENT on Node 22 Windows. Use plain path.\n` +
  `    return command;`;

if (!src.includes(old)) {
  console.log("[patch] WARN: pattern not found — SDK version changed? inspect manually");
  process.exit(1);
}

writeFileSync(file, src.replace(old, next, 1), "utf8");
console.log("[patch] applied OK");
