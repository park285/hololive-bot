import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";

const html = readFileSync(
	fileURLToPath(new URL("../index.html", import.meta.url)),
	"utf8",
);

test("index.html has no inline script (CSP script-src 'self' blocks inline)", () => {
	const scripts = html.match(/<script\b[^>]*>/gi) ?? [];
	for (const tag of scripts) {
		assert.ok(
			/\bsrc\s*=/.test(tag),
			`inline <script> without src would be blocked by CSP: ${tag}`,
		);
	}
});
