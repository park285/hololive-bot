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

test("useWebSocket resets connection state before opening a new socket", () => {
	assert.match(
		source,
		/setState\(\(prev\)\s*=>\s*\(\{\s*\.\.\.prev,\s*isConnected:\s*false,\s*isConnecting:\s*true,\s*error:\s*null,?\s*}\)\);/,
	);
});

test("useWebSocket keeps mount lifetime separate from connection cleanup", () => {
	const mountLifetimeEffect = source.match(
		/useEffect\(\(\)\s*=>\s*{\s*isMountedRef\.current\s*=\s*true;\s*return\s*\(\)\s*=>\s*{\s*isMountedRef\.current\s*=\s*false;\s*};\s*},\s*\[\]\s*\);/,
	);
	assert.ok(mountLifetimeEffect);

	const connectionEffect = source.match(
		/useEffect\(\(\)\s*=>\s*{\s*if\s*\(autoConnect\s*&&\s*url\)\s*{\s*connect\(\);\s*}\s*return\s*\(\)\s*=>\s*{([\s\S]*?)};\s*},\s*\[([^\]]+)]\s*\);/,
	);
	assert.ok(connectionEffect);
	assert.ok(
		source.indexOf(mountLifetimeEffect[0]) < source.indexOf(connectionEffect[0]),
		"mount cleanup must run before connection cleanup on unmount",
	);
	assert.match(connectionEffect[1], /disconnect\(\);/);
	assert.doesNotMatch(connectionEffect[1], /isMountedRef\.current\s*=\s*false/);
	assert.match(connectionEffect[2], /\bconnect\b/);
	assert.match(connectionEffect[2], /\bdisconnect\b/);
	assert.match(connectionEffect[2], /autoConnect/);
	assert.match(connectionEffect[2], /url/);
});
