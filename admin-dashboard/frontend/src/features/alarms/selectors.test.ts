import assert from "node:assert/strict";
import test from "node:test";
import { filterAlarmGroups, groupAlarms } from "./selectors";
import type { Alarm } from "./types";

test("groupAlarms groups the upstream room/channel subscriptions by room", () => {
	const alarms: Alarm[] = [
		{
			roomId: "r1",
			roomName: "Room A",
			channelId: "c1",
			memberName: "Mio",
		},
		{
			roomId: "r1",
			roomName: "Room A",
			channelId: "c2",
			memberName: "Sora",
		},
		{
			roomId: "r2",
			roomName: "Room B",
			channelId: "c3",
			memberName: "Suisei",
		},
	];

	const groups = groupAlarms(alarms);
	assert.equal(groups.length, 2);
	assert.equal(groups[0]?.roomId, "r1");
	assert.equal(groups[0]?.alarms.length, 2);
});

test("filterAlarmGroups matches room and member keywords", () => {
	const groups = groupAlarms([
		{
			roomId: "r1",
			roomName: "Hololive Room",
			channelId: "c1",
			memberName: "Miko",
		},
		{
			roomId: "r2",
			roomName: "Other Room",
			channelId: "c2",
			memberName: "Suisei",
		},
	]);

	assert.equal(filterAlarmGroups(groups, "hololive").length, 1);
	assert.equal(filterAlarmGroups(groups, "other").length, 1);
	assert.equal(filterAlarmGroups(groups, "suisei").length, 1);
	assert.equal(filterAlarmGroups(groups, "").length, 2);
});
