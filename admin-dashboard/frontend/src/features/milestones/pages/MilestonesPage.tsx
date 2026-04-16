import { useQuery } from "@tanstack/react-query";
import Award from "lucide-react/dist/esm/icons/award";
import BellOff from "lucide-react/dist/esm/icons/bell-off";
import Loader2 from "lucide-react/dist/esm/icons/loader-2";
import TrendingUp from "lucide-react/dist/esm/icons/trending-up";
import Trophy from "lucide-react/dist/esm/icons/trophy";
import { queryKeys } from "@/api/queryKeys";
import { milestonesApi } from "@/features/milestones/api";
import { AchievedMilestonesSection } from "@/features/milestones/components/AchievedMilestonesSection";
import {
	type MilestonesStatCard,
	MilestonesStatsSection,
} from "@/features/milestones/components/MilestonesStatsSection";
import { NearMilestonesSection } from "@/features/milestones/components/NearMilestonesSection";

export const MilestonesPage = () => {
	const { data: stats, isLoading: isStatsLoading } = useQuery({
		queryKey: queryKeys.milestones.stats,
		queryFn: milestonesApi.getStats,
		staleTime: 30000,
		refetchInterval: 60000,
	});

	const { data: nearData, isLoading: isNearLoading } = useQuery({
		queryKey: queryKeys.milestones.near,
		queryFn: () => milestonesApi.getNear(0.9),
		staleTime: 30000,
		refetchInterval: 60000,
	});

	const { data: achievedData, isLoading: isAchievedLoading } = useQuery({
		queryKey: queryKeys.milestones.all,
		queryFn: () => milestonesApi.getAchieved({ limit: 20 }),
		staleTime: 60000,
		refetchInterval: 120000,
	});

	const isLoading = isStatsLoading || isNearLoading || isAchievedLoading;

	if (isLoading) {
		return (
			<div className="flex justify-center items-center h-64 text-slate-400">
				<div className="animate-spin mr-2">
					<Loader2 />
				</div>
				마일스톤 데이터를 불러오는 중…
			</div>
		);
	}

	const statCards: MilestonesStatCard[] = [
		{
			label: "총 달성 기록",
			value: stats?.stats.totalAchieved ?? 0,
			variant: "indigo",
			icon: <Trophy size={24} />,
		},
		{
			label: "달성 임박",
			value: stats?.stats.totalNearMilestone ?? 0,
			variant: "yellow",
			icon: <TrendingUp size={24} />,
		},
		{
			label: "최근 달성 (30일)",
			value: stats?.stats.recentAchievements ?? 0,
			variant: "green",
			icon: <Award size={24} />,
		},
		{
			label: "아직 알림 안보냄",
			value: stats?.stats.notNotifiedCount ?? 0,
			variant: "rose",
			icon: <BellOff size={24} />,
		},
	];

	return (
		<div className="space-y-8">
			<div>
				<div className="flex flex-col gap-2 mb-2">
					<h2 className="text-2xl font-bold text-slate-800 tracking-tight">
						Milestone Tracker
					</h2>
					<p className="text-slate-500">
						구독자 마일스톤 달성 현황 및 임박 멤버 모니터링
					</p>
				</div>
			</div>

			<MilestonesStatsSection cards={statCards} />

			<div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
				<NearMilestonesSection nearData={nearData} />
				<AchievedMilestonesSection achievedData={achievedData} />
			</div>
		</div>
	);
};
