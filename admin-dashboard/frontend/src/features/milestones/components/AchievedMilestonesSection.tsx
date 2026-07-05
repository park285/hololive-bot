import { useQuery } from "@tanstack/react-query";
import Trophy from "lucide-react/dist/esm/icons/trophy.mjs";
import { queryKeys } from "@/api/queryKeys";
import { Badge } from "@/components/ui/Badge";
import { QuerySection } from "@/components/ui/QuerySection";
import { Skeleton } from "@/components/ui/Skeleton";
import { milestonesApi } from "@/features/milestones/api";
import { sectionStateProps } from "@/lib/queryState";

const dateFormatter = new Intl.DateTimeFormat("ko-KR", {
	year: "numeric",
	month: "2-digit",
	day: "2-digit",
});

const formatAchievedDate = (value: string) => {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? "날짜 없음" : dateFormatter.format(date);
};

const ListSkeleton = () => (
	<div className="bg-card rounded-2xl border border-border shadow-sm p-4 space-y-3">
		<Skeleton className="h-16 rounded-xl" />
		<Skeleton className="h-16 rounded-xl" />
		<Skeleton className="h-16 rounded-xl" />
	</div>
);

export const AchievedMilestonesSection = () => {
	const query = useQuery({
		queryKey: queryKeys.milestones.all,
		queryFn: () => milestonesApi.getAchieved({ limit: 20 }),
		staleTime: 60000,
		refetchInterval: 120000,
	});

	const achievedData = query.data;

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between pb-2 border-b border-border">
				<h3 className="text-lg font-bold text-foreground flex items-center gap-2">
					<span className="flex items-center justify-center w-9 h-9 rounded-xl bg-linear-to-br from-indigo-400 to-indigo-500 text-white shadow-sm shadow-indigo-200/50">
						<Trophy size={18} />
					</span>
					최근 달성 기록
				</h3>
			</div>

			<QuerySection
				{...sectionStateProps(query)}
				skeleton={<ListSkeleton />}
				isEmpty={(achievedData?.milestones.length ?? 0) === 0}
				emptyContent={
					<div className="bg-card rounded-2xl border border-border shadow-sm overflow-hidden">
						<div className="text-center py-12 text-muted-foreground">
							최근 달성 기록이 없습니다.
						</div>
					</div>
				}
			>
				<div className="bg-card rounded-2xl border border-border shadow-sm overflow-hidden">
					<div className="divide-y divide-border-subtle">
						{achievedData?.milestones.map((milestone, idx) => (
							<div
								key={`${milestone.channelId}-${String(milestone.value)}-${String(idx)}`}
								className="p-4 hover:bg-linear-to-r hover:from-indigo-50/40 hover:to-transparent transition-colors flex items-center justify-between"
							>
								<div className="flex items-center gap-4">
									<div className="w-10 h-10 rounded-full bg-linear-to-br from-indigo-400 to-indigo-500 text-white flex items-center justify-center font-bold shadow-sm shadow-indigo-200/50">
										#{idx + 1}
									</div>
									<div>
										<div className="font-bold text-foreground">
											{milestone.memberName}
										</div>
										<div className="text-sm text-muted-foreground">
											{milestone.value.toLocaleString()} {milestone.type}
										</div>
									</div>
								</div>
								<div className="text-right">
									<div className="text-xs text-subtle-foreground mb-1">
										{formatAchievedDate(milestone.achievedAt)}
									</div>
									<Badge
										variant={milestone.notified ? "default" : "outline"}
										className={
											milestone.notified
												? "bg-emerald-500 hover:bg-emerald-600"
												: "text-amber-500 border-amber-500"
										}
									>
										{milestone.notified ? "알림 완료" : "대기 중"}
									</Badge>
								</div>
							</div>
						))}
					</div>
				</div>
			</QuerySection>
		</div>
	);
};
