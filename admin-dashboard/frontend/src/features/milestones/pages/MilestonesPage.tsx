import { useQuery } from "@tanstack/react-query";
import Award from "lucide-react/dist/esm/icons/award.mjs";
import BellOff from "lucide-react/dist/esm/icons/bell-off.mjs";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import TrendingUp from "lucide-react/dist/esm/icons/trending-up.mjs";
import Trophy from "lucide-react/dist/esm/icons/trophy.mjs";
import { queryKeys } from "@/api/queryKeys";
import { milestonesApi } from "@/features/milestones/api";
import { AchievedMilestonesSection } from "@/features/milestones/components/AchievedMilestonesSection";
import {
	type MilestonesStatCard,
	MilestonesStatsSection,
} from "@/features/milestones/components/MilestonesStatsSection";
import { NearMilestonesSection } from "@/features/milestones/components/NearMilestonesSection";

export const MilestonesPage = () => {
	const {
		data: stats,
		isLoading: isStatsLoading,
		isError: isStatsError,
		error: statsError,
		refetch: refetchStats,
	} = useQuery({
		queryKey: queryKeys.milestones.stats,
		queryFn: milestonesApi.getStats,
		staleTime: 30000,
		refetchInterval: 60000,
	});

	const {
		data: nearData,
		isLoading: isNearLoading,
		isError: isNearError,
		error: nearError,
		refetch: refetchNear,
	} = useQuery({
		queryKey: queryKeys.milestones.near,
		queryFn: () => milestonesApi.getNear(0.9),
		staleTime: 30000,
		refetchInterval: 60000,
	});

	const {
		data: achievedData,
		isLoading: isAchievedLoading,
		isError: isAchievedError,
		error: achievedError,
		refetch: refetchAchieved,
	} = useQuery({
		queryKey: queryKeys.milestones.all,
		queryFn: () => milestonesApi.getAchieved({ limit: 20 }),
		staleTime: 60000,
		refetchInterval: 120000,
	});

	const isLoading = isStatsLoading || isNearLoading || isAchievedLoading;
	const hasError = isStatsError || isNearError || isAchievedError;
	const firstError = statsError ?? nearError ?? achievedError;

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

	if (hasError) {
		return (
			<div className="rounded-2xl border border-rose-100 bg-rose-50 p-8 text-center text-rose-600">
				<p className="font-bold">마일스톤 데이터를 불러오지 못했습니다.</p>
				<p className="mt-2 text-sm">
					{firstError instanceof Error
						? firstError.message
						: "잠시 후 다시 시도해주세요."}
				</p>
				<button
					type="button"
					onClick={() => {
						void Promise.all([refetchStats(), refetchNear(), refetchAchieved()]);
					}}
					className="mt-4 rounded-lg bg-rose-600 px-4 py-2 text-sm font-bold text-white hover:bg-rose-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-300"
				>
					다시 시도
				</button>
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
