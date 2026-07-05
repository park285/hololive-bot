import { useQuery } from "@tanstack/react-query";
import Award from "lucide-react/dist/esm/icons/award.mjs";
import BellOff from "lucide-react/dist/esm/icons/bell-off.mjs";
import TrendingUp from "lucide-react/dist/esm/icons/trending-up.mjs";
import Trophy from "lucide-react/dist/esm/icons/trophy.mjs";
import type { ReactNode } from "react";
import { queryKeys } from "@/api/queryKeys";
import { QuerySection } from "@/components/ui/QuerySection";
import { Skeleton } from "@/components/ui/Skeleton";
import { StatCard } from "@/components/ui/StatCard";
import { milestonesApi } from "@/features/milestones/api";
import { visibleRefetchInterval } from "@/lib/polling";
import { sectionStateProps } from "@/lib/queryState";

interface MilestonesStatCard {
	label: string;
	value: number;
	variant: "indigo" | "yellow" | "green" | "rose";
	icon: ReactNode;
}

const SKELETON_KEYS = ["a", "b", "c", "d"];

const StatsSkeleton = () => (
	<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
		{SKELETON_KEYS.map((key) => (
			<Skeleton key={key} className="h-32 rounded-2xl" />
		))}
	</div>
);

export const MilestonesStatsSection = () => {
	const query = useQuery({
		queryKey: queryKeys.milestones.stats,
		queryFn: milestonesApi.getStats,
		staleTime: 30000,
		refetchInterval: visibleRefetchInterval(60000),
	});

	const stats = query.data;
	const cards: MilestonesStatCard[] = [
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
		<QuerySection {...sectionStateProps(query)} skeleton={<StatsSkeleton />}>
			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
				{cards.map((card) => (
					<div key={card.label}>
						<StatCard
							label={card.label}
							value={card.value}
							icon={card.icon}
							variant={card.variant}
						/>
					</div>
				))}
			</div>
		</QuerySection>
	);
};
