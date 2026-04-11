import apiClient from "@/api/client";
import type { YouTubeCommunityShortsOpsResponse } from "@/features/youtube-ops/types";

export const youtubeOpsApi = {
	get: async () =>
		(
			await apiClient.get<YouTubeCommunityShortsOpsResponse>(
				"/admin/api/holo/stats/youtube/community-shorts",
			)
		).data,
};
