import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const boundaryFiles = [
  new URL("./contracts.d.ts", import.meta.url),
  new URL("./rpc-boundary.mjs", import.meta.url),
  new URL("./rpc-validation.mjs", import.meta.url),
  new URL("./server.mjs", import.meta.url),
  new URL("./helper-runtime.mjs", import.meta.url),
  new URL("./real-fetchers.mjs", import.meta.url),
];

test("RPC boundary forbids explicit any", async () => {
  for (const file of boundaryFiles) {
    const source = await readFile(file, "utf8");
    assert.doesNotMatch(source, /\bany\b/, file.pathname);
  }
});

test("RPC boundary strict config covers server wiring", async () => {
  const config = /** @type {unknown} */ (
    JSON.parse(await readFile(new URL("../tsconfig.json", import.meta.url), "utf8"))
  );
  assert.ok(typeof config === "object" && config !== null && !Array.isArray(config));
  assert.equal(config.compilerOptions.strict, true);
  assert.equal(config.compilerOptions.checkJs, true);
  assert.equal(config.compilerOptions.noImplicitAny, true);
  assert.equal(config.compilerOptions.strictNullChecks, true);
  assert.ok(config.include.includes("src/server.mjs"));
  assert.ok(config.include.includes("src/helper-runtime.mjs"));
  assert.ok(config.include.includes("src/real-fetchers.mjs"));
  assert.ok(config.include.includes("src/rpc-boundary.mjs"));
  assert.ok(config.include.includes("src/rpc-validation.mjs"));
});
