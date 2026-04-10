import assert from "node:assert/strict";
import test from "node:test";
import { filterAlarmGroups, groupAlarms } from "./selectors";
import type { Alarm } from "./types";

test("groupAlarms groups by room and user", () => {
	const alarms: Alarm[] = [
		{
			roomId: "r1",
			roomName: "Room A",
			userId: "u1",
			userName: "User A",
			channelId: "c1",
			memberName: "Mio",
		},
		{
			roomId: "r1",
			roomName: "Room A",
			userId: "u1",
			userName: "User A",
			channelId: "c2",
			memberName: "Sora",
		},
		{
			roomId: "r2",
			roomName: "Room B",
			userId: "u2",
			userName: "User B",
			channelId: "c3",
			memberName: "Suisei",
		},
	];

	const groups = groupAlarms(alarms);
	assert.equal(groups.length, 2);
	assert.equal(groups[0]?.roomId, "r1");
	assert.equal(groups[0]?.alarms.length, 2);
});

test("filterAlarmGroups matches room, user, and member keywords", () => {
	const groups = groupAlarms([
		{
			roomId: "r1",
			roomName: "Hololive Room",
			userId: "u1",
			userName: "Alpha",
			channelId: "c1",
			memberName: "Miko",
		},
		{
			roomId: "r2",
			roomName: "Other Room",
			userId: "u2",
			userName: "Beta",
			channelId: "c2",
			memberName: "Suisei",
		},
	]);

	assert.equal(filterAlarmGroups(groups, "hololive").length, 1);
	assert.equal(filterAlarmGroups(groups, "beta").length, 1);
	assert.equal(filterAlarmGroups(groups, "suisei").length, 1);
	assert.equal(filterAlarmGroups(groups, "").length, 2);
});
