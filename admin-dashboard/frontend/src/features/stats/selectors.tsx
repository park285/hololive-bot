import Bell from "lucide-react/dist/esm/icons/bell";
import MessageSquare from "lucide-react/dist/esm/icons/message-square";
import Users from "lucide-react/dist/esm/icons/users";
import type { AggregatedStatus } from "@/api/core";
import type { StatsOverviewCard } from "@/features/stats/components/StatsOverviewSection";
import type {
	ChannelStat,
	ChannelStatsResponse,
	StatsResponse,
} from "@/features/stats/types";

const DEFAULT_TOP_CHANNEL_LIMIT = 10;

export function buildCurrentServiceStats(
	statusData: AggregatedStatus | undefined,
	holoStats: StatsResponse | undefined,
	selectedService: string,
) {
	const baseService = statusData?.services.find(
		(service: AggregatedStatus["services"][number]) =>
			service.name === selectedService,
	);

	const runtimeInfo =
		selectedService === "hololive-bot"
			? {
					version: holoStats?.version,
					uptime: holoStats?.uptime,
				}
			: selectedService === "admin-dashboard"
				? {
						version: statusData?.version,
						uptime: statusData?.uptime,
					}
				: {
						version: undefined,
						uptime: undefined,
					};

	return {
		name: selectedService,
		available: baseService?.available ?? false,
		version: runtimeInfo.version ?? "-",
		uptime: runtimeInfo.uptime ?? "-",
	};
}

export function buildMainStats(
	holoStats: StatsResponse | undefined,
): StatsOverviewCard[] {
	return [
		{
			label: "등록된 멤버",
			value: holoStats?.members ?? 0,
			variant: "cyan",
			icon: <Users size={24} />,
		},
		{
			label: "활성 알람",
			value: holoStats?.alarms ?? 0,
			variant: "rose",
			icon: <Bell size={24} />,
		},
		{
			label: "연동된 방",
			value: holoStats?.rooms ?? 0,
			variant: "indigo",
			icon: <MessageSquare size={24} />,
		},
	];
}

export function selectTopChannelStats(
	response: ChannelStatsResponse | undefined,
	limit = DEFAULT_TOP_CHANNEL_LIMIT,
): ChannelStat[] {
	const stats = response?.stats ?? {};
	return Object.values(stats)
		.filter((stat): stat is NonNullable<typeof stat> => stat != null)
		.sort((first, second) => second.SubscriberCount - first.SubscriberCount)
		.slice(0, limit);
}
