import apiClient from "@/api/client";
import type { ChannelStatsResponse, StatsResponse } from "./types";

const TOP_CHANNEL_LIMIT = 10;

export const statsApi = {
	get: async () => (await apiClient.get<StatsResponse>("/holo/stats")).data,
	getChannels: async () =>
		(
			await apiClient.get<ChannelStatsResponse>("/holo/stats/channels", {
				params: { limit: TOP_CHANNEL_LIMIT },
			})
		).data,
};
