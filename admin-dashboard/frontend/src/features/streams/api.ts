import { adminClient } from "@/api/adminClient";
import type { StreamOrg } from "./types";

export const streamsApi = {
	getLive: async (org: StreamOrg = "hololive") => {
		const response = (await adminClient.holoGetLiveStreams({ org })).data;
		return {
			...response,
			streams: Array.isArray(response.streams) ? response.streams : [],
		};
	},
	getUpcoming: async (org: StreamOrg = "hololive") => {
		const response = (await adminClient.holoGetUpcomingStreams({ org })).data;
		return {
			...response,
			streams: Array.isArray(response.streams) ? response.streams : [],
		};
	},
};
