import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));

const readSource = (filename: string) =>
	readFileSync(path.join(dirname, filename), "utf8");

const collectSourceFiles = (directory: string): string[] =>
	readdirSync(directory).flatMap((entry) => {
		const fullPath = path.join(directory, entry);
		const stats = statSync(fullPath);

		if (stats.isDirectory()) {
			return collectSourceFiles(fullPath);
		}

		return fullPath.endsWith(".ts") || fullPath.endsWith(".tsx")
			? [fullPath]
			: [];
	});

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

test("critical-path api wrappers stay on the root axios client without an extra holoClient layer", () => {
	const coreSource = readSource("core.ts");

	assert.equal(coreSource.includes("handleSessionStatus"), false);
	assert.equal(coreSource.includes("handleDockerHealth"), false);
	assert.equal(coreSource.includes("handleAggregatedStatus"), false);
	assert.match(coreSource, /from ["']\.\/client["']/);
	assert.equal(existsSync(path.join(dirname, "holoClient.ts")), false);
});

test("stats api keeps dashboard reads on the root axios client and requests top-limited channels", () => {
	const statsSource = readSource("../features/stats/api.ts");

	assert.equal(statsSource.includes("adminClient"), false);
	assert.match(statsSource, /from ["']@\/api\/client["']/);
	assert.match(statsSource, /limit:\s*TOP_CHANNEL_LIMIT/);
});

test("vite dev proxy forwards websocket upgrades for admin api routes", () => {
	const viteConfigSource = readSource("../../vite.config.ts");

	assert.match(viteConfigSource, /['"]\/admin\/api['"]\s*:\s*\{/);
	assert.match(viteConfigSource, /ws:\s*true/);
});

test("system stats websocket keeps the stream server-push only without client ping chatter", () => {
	const systemStatsHistorySource = readSource(
		"../features/stats/hooks/useSystemStatsHistory.ts",
	);

	assert.match(systemStatsHistorySource, /enablePing:\s*false/);
	assert.match(systemStatsHistorySource, /visibilitychange/);
	assert.match(systemStatsHistorySource, /isVisible/);
	assert.match(systemStatsHistorySource, /startTransition/);
	assert.match(systemStatsHistorySource, /Intl\.DateTimeFormat/);
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

test("frontend source keeps explicit any blocked while generated files stay isolated", () => {
	const eslintSource = readSource("../../eslint.config.js");
	const tsconfigSource = readSource("../../tsconfig.app.json");
	const sourceFiles = collectSourceFiles(path.join(dirname, ".."))
		.filter((file) => !file.includes(`${path.sep}api${path.sep}generated${path.sep}`))
		.filter((file) => !file.endsWith(".test.ts") && !file.endsWith(".test.tsx"));

	assert.match(eslintSource, /@typescript-eslint\/no-explicit-any["']?:\s*['"]error['"]/);
	assert.match(eslintSource, /@typescript-eslint\/no-unsafe-assignment["']?:\s*['"]error['"]/);
	assert.equal(tsconfigSource.includes('"noImplicitAny": true'), true);

	for (const file of sourceFiles) {
		const source = readFileSync(file, "utf8");
		assert.equal(/\bany\b/.test(source), false, file);
	}
});

test("development tooling wires react-query devtools and opt-in msw bootstrap", () => {
	const appSource = readSource("../App.tsx");
	const mainSource = readSource("../main.tsx");

	assert.match(appSource, /react-query-devtools/);
	assert.match(appSource, /import\.meta\.env\.DEV/);
	assert.match(mainSource, /VITE_ENABLE_MSW/);
	assert.match(mainSource, /mocks\/browser/);
});

test("large frontend lists route through the shared VirtualList helper", () => {
	const alarmsSource = readSource("../features/alarms/components/AlarmGroups.tsx");
	const dockerSource = readSource("../components/settings/DockerContainerList.tsx");
	const roomsSource = readSource("../features/rooms/components/RoomsListSection.tsx");
	const membersSource = readSource("../features/members/components/MembersGrid.tsx");
	const liveSource = readSource("../features/streams/components/LiveStreamsSection.tsx");
	const upcomingSource = readSource(
		"../features/streams/components/UpcomingStreamsSection.tsx",
	);

	for (const source of [
		alarmsSource,
		dockerSource,
		roomsSource,
		membersSource,
		liveSource,
		upcomingSource,
	]) {
		assert.match(source, /VirtualList/);
		assert.match(source, /<VirtualList/);
	}
});
