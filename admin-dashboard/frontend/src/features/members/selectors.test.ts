import assert from "node:assert/strict";
import test from "node:test";
import { cloneMembers, filterMembers, sortMembers } from "./selectors";
import type { Member } from "./types";

const members: Member[] = [
	{
		id: 1,
		name: "Suisei",
		channelId: "UC1",
		aliases: { ko: ["스이세이"], ja: ["すいせい"] },
		nameKo: "스이세이",
		nameJa: "すいせい",
		isGraduated: false,
	},
	{
		id: 2,
		name: "Aloe",
		channelId: "UC2",
		aliases: { ko: ["알로에"], ja: ["アロエ"] },
		nameKo: "알로에",
		nameJa: "アロエ",
		isGraduated: true,
	},
];

test("cloneMembers copies alias arrays", () => {
	const cloned = cloneMembers(members);
	cloned[0]?.aliases.ko.push("별칭");
	assert.equal(members[0]?.aliases.ko.includes("별칭"), false);
});

test("filterMembers respects hideGraduated and alias search", () => {
	assert.equal(filterMembers(members, "", true).length, 1);
	assert.equal(filterMembers(members, "알로에", false).length, 1);
	assert.equal(filterMembers(members, "すいせい", false).length, 1);
});

test("sortMembers keeps active members before graduated ones", () => {
	const sorted = sortMembers([...members].reverse());
	assert.equal(sorted[0]?.isGraduated, false);
	assert.equal(sorted[1]?.isGraduated, true);
});
