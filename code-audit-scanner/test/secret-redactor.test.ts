import assert from "node:assert/strict";
import { test } from "node:test";
import { redactSecrets } from "../src/security/secret-redactor.js";

test("redacts openai-like keys, bearer tokens, and assignment forms", () => {
  const input =
    "using sk-abcdefghijklmnopqrstuvwxyz012345 Authorization: Bearer abcdefgh.ijklmnop api_key='hunter2' password=\"secret-value\"";
  const output = redactSecrets(input);
  assert.equal(output.includes("sk-abcdefghijklmnopqrstuvwxyz012345"), false);
  assert.equal(output.includes("Bearer abcdefgh.ijklmnop"), false);
  assert.equal(output.includes("hunter2"), false);
  assert.equal(output.includes("secret-value"), false);
  assert.match(output, /REDACTED/);
});

test("leaves ordinary log lines alone", () => {
  const line = "scan 10001 completed with 3 findings in sql-injection/app.py";
  assert.equal(redactSecrets(line), line);
});
