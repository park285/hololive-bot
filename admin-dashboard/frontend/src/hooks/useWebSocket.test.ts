import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(dirname, "useWebSocket.ts"), "utf8");

test("useWebSocket ignores callbacks from sockets it no longer owns", () => {
	const guards = source.match(
		/if\s*\(!isMountedRef\.current\s*\|\|\s*wsRef\.current\s*!==\s*ws\)\s*return;/g,
	);
	assert.ok(guards);
	assert.equal(guards.length, 4);
});

test("useWebSocket clears ownership before closing manually", () => {
	assert.match(
		source,
		/const\s+ws\s*=\s*wsRef\.current;\s*wsRef\.current\s*=\s*null;\s*ws\?\.close\(\);/,
	);
	assert.doesNotMatch(
		source,
		/wsRef\.current\.close\(\);\s*wsRef\.current\s*=\s*null;/,
	);
});

test("useWebSocket cancels stale reconnect callbacks", () => {
	assert.match(
		source,
		/reconnectTimerRef\.current\s*=\s*null;\s*if\s*\(!isMountedRef\.current\s*\|\|\s*wsRef\.current\s*!==\s*null\)\s*return;/,
	);
});
