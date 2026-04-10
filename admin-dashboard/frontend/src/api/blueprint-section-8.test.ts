import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));

const readSource = (filename: string) =>
	readFileSync(path.join(dirname, filename), "utf8");

test("adminClient owns the single generated Admin instance wired to apiClient", () => {
	const source = readSource("adminClient.ts");

	assert.match(source, /new Admin\(\)/);
	assert.match(source, /adminClient\.instance = apiClient/);
});

test("client 401 handler no longer exempts stale holo paths", () => {
	const source = readSource("client.ts");

	assert.equal(source.includes('startsWith("/holo/")'), false);
});

test("api wrappers import the shared adminClient singleton without an extra holoClient layer", () => {
	const coreSource = readSource("core.ts");

	assert.match(coreSource, /from ["']@\/api\/adminClient["']/);
	assert.equal(existsSync(path.join(dirname, "holoClient.ts")), false);
});
