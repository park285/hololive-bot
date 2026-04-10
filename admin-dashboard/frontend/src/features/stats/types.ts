export type {
	ChannelStat,
	ChannelStatsResponse,
	StatsResponse,
} from "@/api/generated/data-contracts";

export interface ServiceGoroutines {
	name: string;
	goroutines: number;
	available: boolean;
}

export interface SystemStats {
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	memoryUsed: number;
	goroutines: number;
	totalGoroutines: number;
	serviceGoroutines: ServiceGoroutines[];
}
