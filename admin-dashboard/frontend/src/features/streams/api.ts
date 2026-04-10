import { holoClient } from "@/api/holoClient";
import type { StreamOrg } from "./types";

export const streamsApi = {
	getLive: async (org: StreamOrg = "hololive") => {
		const response = await holoClient.getLiveStreams(org);
		return {
			...response,
			streams: Array.isArray(response.streams) ? response.streams : [],
		};
	},
	getUpcoming: async (org: StreamOrg = "hololive") => {
		const response = await holoClient.getUpcomingStreams(org);
		return {
			...response,
			streams: Array.isArray(response.streams) ? response.streams : [],
		};
	},
};
