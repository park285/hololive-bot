import apiClient from "@/api/client";
import type { StatsResponse } from "./types";

export const statsApi = {
	get: async () => (await apiClient.get<StatsResponse>("/holo/stats")).data,
};
