import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import type { AggregatedStatus } from "../../api/core";
import {
	buildCurrentServiceStats,
	buildMainStats,
	selectTopChannelStats,
} from "./selectors";
import type { ChannelStatsResponse, StatsResponse } from "./types";

(globalThis as { React?: typeof React }).React = React;

test("buildCurrentServiceStats prefers holo runtime fields for hololive-bot", () => {
	const status = {
		services: [{ name: "hololive-bot", available: true }],
		version: "admin-v1",
		uptime: "1h",
	} as AggregatedStatus;

	const holo = {
		status: "ok",
		members: 10,
		alarms: 20,
		rooms: 3,
		version: "bot-v2",
		uptime: "2h",
	} as StatsResponse;

	const current = buildCurrentServiceStats(status, holo, "hololive-bot");
	assert.equal(current.version, "bot-v2");
	assert.equal(current.uptime, "2h");
	assert.equal(current.available, true);
});

test("buildMainStats maps summary counts", () => {
	const cards = buildMainStats({
		status: "ok",
		members: 11,
		alarms: 22,
		rooms: 33,
		version: "v1",
		uptime: "3h",
	} as StatsResponse);

	assert.deepEqual(
		cards.map((card) => card.value),
		[11, 22, 33],
	);
});

test("selectTopChannelStats sorts subscribers descending and trims to top 10", () => {
	const stats = Object.fromEntries(
		Array.from({ length: 12 }, (_, index) => {
			const subscriberCount = 1_000 + index;
			return [
				`channel-${index + 1}`,
				{
					ChannelID: `channel-${index + 1}`,
					ChannelTitle: `Channel ${index + 1}`,
					SubscriberCount: subscriberCount,
					ViewCount: subscriberCount * 10,
					VideoCount: index + 1,
				},
			];
		}),
	) as ChannelStatsResponse["stats"];

	const topStats = selectTopChannelStats({
		status: "ok",
		stats,
	});

	assert.equal(topStats.length, 10);
	assert.equal(topStats[0]?.ChannelID, "channel-12");
	assert.equal(topStats[9]?.ChannelID, "channel-3");
});
