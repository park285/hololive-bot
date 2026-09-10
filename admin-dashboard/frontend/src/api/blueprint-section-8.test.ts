import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

// SDK 연결과 호출 횟수는 transport.test.ts와 msw.integration.test.ts의 실행형 시험으로 검증한다.
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



test("client 401 handler no longer exempts stale holo paths", () => {
	const source = readSource("client.ts");

	assert.equal(source.includes('startsWith("/holo/")'), false);
});





test("vite dev proxy forwards websocket upgrades for admin api routes", () => {
	const viteConfigSource = readSource("../../vite.config.ts");

	assert.match(viteConfigSource, /['"]\/admin\/api['"]\s*:\s*\{/);
	assert.match(viteConfigSource, /ws:\s*true/);
});

// WS frame 수신·잘못된 schema 거부·visibility 수명은 실제 browser ledger로 검증합니다.

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
		const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
		const visit = (node: ts.Node): void => {
			assert.notEqual(node.kind, ts.SyntaxKind.AnyKeyword, file);
			ts.forEachChild(node, visit);
		};
		visit(ast);
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
	const dockerSource = readSource("../features/docker/components/ContainerList.tsx");
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

test("repeated destructive controls have contextual accessible names", () => {
	const badgeSource = readSource("../components/ui/Badge.tsx");
	const memberCardSource = readSource("../components/MemberCard.tsx");

	assert.equal(badgeSource.includes('aria-label="삭제"'), false);
	assert.match(badgeSource, /aria-label=\{removeAriaLabel\}/);
	assert.match(
		memberCardSource,
		/removeAriaLabel=\{`\$\{member\.name\} 한국어 별명 \$\{alias\} 제거`\}/,
	);
	assert.match(
		memberCardSource,
		/removeAriaLabel=\{`\$\{member\.name\} 일본어 별명 \$\{alias\} 제거`\}/,
	);
});

// 알람 그룹의 native button·키보드·편집/펼침 분리는 session-tabs.browser.test.mjs에서 실제 렌더링으로 검증합니다.
