import assert from "node:assert/strict";
import { test } from "node:test";
import {
  validateIncludePaths,
  normalizeIncludePath,
} from "../src/contract/validate-task.js";

test("includePaths accepts relative paths", () => {
  validateIncludePaths(["backend/src", "api/"]);
  validateIncludePaths(undefined);
});

test("includePaths rejects absolute and drive paths", () => {
  assert.throws(() => validateIncludePaths(["F:\\dev\\x"]), /relative/);
  assert.throws(() => validateIncludePaths(["/etc/passwd"]), /relative/);
});

test("includePaths rejects traversal", () => {
  assert.throws(() => validateIncludePaths(["../outside"]), /'\.\.'/);
  assert.throws(() => validateIncludePaths(["a/../../b"]), /'\.\.'/);
});

test("includePaths rejects empty and duplicates", () => {
  assert.throws(() => validateIncludePaths([""]), /nonempty/);
  assert.throws(() => validateIncludePaths(["a/b", "a\\b"]), /duplicate/);
});

test("normalizeIncludePath unifies separators and trailing slash", () => {
  assert.equal(normalizeIncludePath("a\\b/"), "a/b");
  assert.equal(normalizeIncludePath("x"), "x");
});
