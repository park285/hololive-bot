import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));

const readSource = (filename: string) =>
	readFileSync(path.join(dirname, filename), "utf8");

test("adminClient owns the single generated Admin instance wired to root api client", () => {
	const source = readSource("adminClient.ts");

	assert.match(source, /new Admin\(\)/);
	assert.match(source, /createApiClient\(""\)/);
	assert.equal(source.includes("adminClient.instance = apiClient"), false);
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

test("dead SSR helpers are removed from the frontend bundle", () => {
	assert.equal(existsSync(path.join(dirname, "../hooks/useSSRData.ts")), false);
	assert.equal(existsSync(path.join(dirname, "../utils/ssr.ts")), false);
});

test("members and settings pages no longer depend on SSR-derived initial data", () => {
	const membersPageSource = readSource("../features/members/hooks/useMembersPage.ts");
	const settingsPageSource = readSource("../features/settings/pages/SettingsPage.tsx");

	assert.equal(membersPageSource.includes("useSSRData"), false);
	assert.equal(membersPageSource.includes("initialData:"), false);
	assert.equal(settingsPageSource.includes("useSSRData"), false);
	assert.equal(settingsPageSource.includes("initialData={"), false);
	assert.equal(settingsPageSource.includes("initialHealth={"), false);
	assert.equal(settingsPageSource.includes("initialContainers={"), false);
});
