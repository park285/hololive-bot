export type {
	Milestone,
	MilestoneStats,
	MilestoneStatsResponse,
	MilestonesResponse,
	NearMilestone,
	NearMilestonesResponse,
} from "@/api/generated/data-contracts";

export interface GetMilestonesParams {
	limit?: number;
	offset?: number;
	channelId?: string;
	memberName?: string;
}
