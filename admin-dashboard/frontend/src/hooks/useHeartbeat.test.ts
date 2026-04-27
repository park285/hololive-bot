import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(dirname, "useHeartbeat.ts"), "utf8");

test("useHeartbeat sends an immediate idle heartbeat when idle state changes", () => {
	assert.match(
		source,
		/useEffect\(\(\)\s*=>\s*{[\s\S]*if\s*\(!isAuthenticated\)\s*return;[\s\S]*(?:if\s*\(isIdle\)\s*{\s*void\s+sendHeartbeat\(true\);\s*}|void\s+sendHeartbeat\(isIdle\);)[\s\S]*}\s*,\s*\[[^\]]*isAuthenticated[^\]]*isIdle[^\]]*sendHeartbeat[^\]]*\]\s*\)/,
	);
});

test("useHeartbeat logs the user out immediately when heartbeat returns idle_rejected", () => {
	const idleRejectedBranch = source.match(
		/if\s*\(response\.idle_rejected\)\s*{([\s\S]*?)}\s*if\s*\(response\.absolute_expired\)/,
	);

	assert.ok(idleRejectedBranch, "idle_rejected branch should exist");
	assert.match(idleRejectedBranch[1], /expireSession\(/);
	assert.match(idleRejectedBranch[1], /response\.error/);
});
