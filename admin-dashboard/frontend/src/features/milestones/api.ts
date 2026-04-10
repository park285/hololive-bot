import { adminClient } from "@/api/adminClient";
import type { GetMilestonesParams } from "./types";

export type { GetMilestonesParams } from "./types";

export const milestonesApi = {
	getAchieved: async (params?: GetMilestonesParams) =>
		(await adminClient.holoGetMilestones({ limit: 50, ...params })).data,
	getNear: async (threshold = 0.9) =>
		(await adminClient.holoGetNearMilestones({ threshold })).data,
	getStats: async () => (await adminClient.holoGetMilestoneStats()).data,
};
