import assert from "node:assert/strict";
import { test } from "node:test";
import {
	buildRoomAccessRows,
	filterRoomAccessRows,
} from "@/features/rooms/selectors";
import type { JoinedRoom } from "@/features/rooms/types";

const joinedRooms: JoinedRoom[] = [
	{
		chatId: "100",
		name: "페코라방",
		type: "OpenChat",
		memberCount: 12,
	},
	{
		chatId: "200",
		name: "스이세이방",
		type: "MultiChat",
		memberCount: 4,
	},
];

test("buildRoomAccessRows merges joined rooms with registered ACL entries", () => {
	const rows = buildRoomAccessRows(["100", "legacy-room"], joinedRooms);

	assert.deepEqual(
		rows.map(({ aclValue, chatId, registered, joined }) => ({
			aclValue,
			chatId,
			registered,
			joined,
		})),
		[
			{ aclValue: "100", chatId: "100", registered: true, joined: true },
			{
				aclValue: "legacy-room",
				chatId: "legacy-room",
				registered: true,
				joined: false,
			},
			{ aclValue: "200", chatId: "200", registered: false, joined: true },
		],
	);
});

test("buildRoomAccessRows preserves a name-based ACL entry for removal", () => {
	const rows = buildRoomAccessRows(["페코라방"], joinedRooms);
	const pekoraRoom = rows.find((row) => row.chatId === "100");

	assert.equal(pekoraRoom?.registered, true);
	assert.equal(pekoraRoom?.aclValue, "페코라방");
});

test("filterRoomAccessRows searches names, joined IDs, and manual ACL values", () => {
	const rows = buildRoomAccessRows(["legacy-room"], joinedRooms);

	assert.deepEqual(
		filterRoomAccessRows(rows, "스이세이").map((row) => row.chatId),
		["200"],
	);
	assert.deepEqual(
		filterRoomAccessRows(rows, "100").map((row) => row.chatId),
		["100"],
	);
	assert.deepEqual(
		filterRoomAccessRows(rows, "LEGACY").map((row) => row.chatId),
		["legacy-room"],
	);
});
