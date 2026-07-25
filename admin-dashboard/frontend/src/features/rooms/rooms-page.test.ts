import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const roomsListSource = readFileSync(
	new URL("./components/RoomsListSection.tsx", import.meta.url),
	"utf8",
);

test("rooms list exposes block and unblock as visible row actions", () => {
	assert.match(roomsListSource, /const addAction = isBlacklist \? "차단" : "허용"/);
	assert.match(
		roomsListSource,
		/const removeAction = isBlacklist \? "차단 해제" : "허용 해제"/,
	);
	assert.doesNotMatch(roomsListSource, /opacity-0|group-hover:opacity-100/);
	assert.doesNotMatch(roomsListSource, /RoomPicker/);
});
