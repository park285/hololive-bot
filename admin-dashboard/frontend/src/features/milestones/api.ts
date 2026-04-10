import { holoClient } from "@/api/holoClient";
import type { GetMilestonesParams } from "./types";

export type { GetMilestonesParams } from "./types";

export const milestonesApi = {
	getAchieved: (params?: GetMilestonesParams) =>
		holoClient.getMilestones({ limit: 50, ...params }),
	getNear: (threshold = 0.9) => holoClient.getNearMilestones(threshold),
	getStats: holoClient.getMilestoneStats,
};
