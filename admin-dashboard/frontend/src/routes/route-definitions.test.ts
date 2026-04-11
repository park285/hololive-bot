import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(dirname, "route-definitions.ts"), "utf8");

const featureRoutes = [
	["StatsPage", "@/features/stats/pages/StatsPage", "@/components/StatsTab"],
	[
		"YouTubeOpsPage",
		"@/features/youtube-ops/pages/YouTubeOpsPage",
		"@/components/YouTubeOpsTab",
	],
	[
		"StreamsPage",
		"@/features/streams/pages/StreamsPage",
		"@/components/StreamsTab",
	],
	[
		"MembersPage",
		"@/features/members/pages/MembersPage",
		"@/components/MembersTab",
	],
	[
		"MilestonesPage",
		"@/features/milestones/pages/MilestonesPage",
		"@/components/MilestonesTab",
	],
	["AlarmsPage", "@/features/alarms/pages/AlarmsPage", "@/components/AlarmsTab"],
	["RoomsPage", "@/features/rooms/pages/RoomsPage", "@/components/RoomsTab"],
	[
		"SettingsPage",
		"@/features/settings/pages/SettingsPage",
		"@/components/SettingsTab",
	],
] as const;

const deletedWrappers = [
	"../components/AlarmsTab.tsx",
	"../components/MembersTab.tsx",
	"../components/MilestonesTab.tsx",
	"../components/RoomsTab.tsx",
	"../components/SettingsTab.tsx",
	"../components/StatsTab.tsx",
	"../components/StreamsTab.tsx",
	"../components/YouTubeOpsTab.tsx",
];

test("dashboard routes lazy-load feature pages directly", () => {
	for (const [, directImport, wrapperImport] of featureRoutes) {
		assert.match(source, new RegExp(directImport.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
		assert.equal(source.includes(wrapperImport), false);
	}
});

test("trivial tab wrapper files are removed", () => {
	for (const relativePath of deletedWrappers) {
		assert.equal(existsSync(path.join(dirname, relativePath)), false, relativePath);
	}
});
