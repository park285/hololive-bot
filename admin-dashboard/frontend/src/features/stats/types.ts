export type {
	ChannelStat,
	ChannelStatsResponse,
	StatsResponse,
} from "@/api/generated/data-contracts";

export type RuntimeMetricKind = "goroutine" | "thread";

export interface ServiceRuntimeStat {
	name: string;
	count: number;
	metricKind: RuntimeMetricKind;
	available: boolean;
	error?: string | null;
}

export interface SystemStats {
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	memoryUsed: number;
	threadCount: number;
	totalGoGoroutines: number;
	totalRuntimeUnits: number;
	serviceRuntime: ServiceRuntimeStat[];
}
