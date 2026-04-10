import { holoClient } from "@/api/holoClient";

export const statsApi = {
	get: holoClient.getStats,
	getChannels: holoClient.getChannelStats,
};
