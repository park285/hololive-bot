import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(dirname, "useHeartbeat.ts"), "utf8");

test("useHeartbeat only releases in-flight ownership for the matching request", () => {
	assert.match(
		source,
		/if\s*\(abortControllerRef\.current\s*===\s*controller\)\s*{[\s\S]*?abortControllerRef\.current\s*=\s*null;[\s\S]*?inFlightRef\.current\s*=\s*false;[\s\S]*?}/,
	);
	assert.doesNotMatch(
		source,
		/if\s*\(abortControllerRef\.current\s*===\s*controller\)\s*{[\s\S]*?abortControllerRef\.current\s*=\s*null;[\s\S]*?}\s*inFlightRef\.current\s*=\s*false;/,
	);
});
