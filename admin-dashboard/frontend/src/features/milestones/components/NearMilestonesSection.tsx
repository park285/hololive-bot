import { useQuery } from "@tanstack/react-query";
import TrendingUp from "lucide-react/dist/esm/icons/trending-up.mjs";
import Video from "lucide-react/dist/esm/icons/video.mjs";
import { queryKeys } from "@/api/queryKeys";
import { Progress } from "@/components/ui/Progress";
import { QuerySection } from "@/components/ui/QuerySection";
import { Skeleton } from "@/components/ui/Skeleton";
import { milestonesApi } from "@/features/milestones/api";
import { visibleRefetchInterval } from "@/lib/polling";
import { sectionStateProps } from "@/lib/queryState";

const normalizePercent = (value: number) => {
	const percent = value <= 1 ? value * 100 : value;
	return Math.min(100, Math.max(0, percent));
};

const ListSkeleton = () => (
	<div className="bg-card rounded-2xl border border-border shadow-sm p-4 space-y-3">
		<Skeleton className="h-16 rounded-xl" />
		<Skeleton className="h-16 rounded-xl" />
		<Skeleton className="h-16 rounded-xl" />
	</div>
);

export const NearMilestonesSection = () => {
	const query = useQuery({
		queryKey: queryKeys.milestones.near,
		queryFn: () => milestonesApi.getNear(0.9),
		staleTime: 30000,
		refetchInterval: visibleRefetchInterval(60000),
	});

	const nearData = query.data;

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between pb-2 border-b border-border">
				<h3 className="text-lg font-bold text-foreground flex items-center gap-2">
					<span className="flex items-center justify-center w-9 h-9 rounded-xl bg-linear-to-br from-amber-400 to-amber-500 text-white shadow-sm shadow-amber-200/50">
						<TrendingUp size={18} />
					</span>
					{nearData?.threshold && nearData.threshold > 0
						? "달성 임박 멤버"
						: "달성 근접 멤버"}
					{nearData?.threshold && nearData.threshold > 0 && (
						<span className="ml-2 text-xs py-1 px-2 bg-amber-50 text-amber-600 rounded-full font-medium">
							진행률 {normalizePercent(nearData.threshold).toFixed(0)}% 이상
						</span>
					)}
				</h3>
				<span className="text-muted-foreground text-sm font-medium">
					{nearData?.count ?? 0}명
				</span>
			</div>

			<QuerySection
				{...sectionStateProps(query)}
				skeleton={<ListSkeleton />}
				isEmpty={(nearData?.members.length ?? 0) === 0}
				emptyContent={
					<div className="bg-card rounded-2xl border border-border shadow-sm overflow-hidden">
						<div className="text-center py-12 text-muted-foreground">
							현재 달성 임박 멤버가 없습니다.
						</div>
					</div>
				}
			>
				<div className="bg-card rounded-2xl border border-border shadow-sm overflow-hidden">
					<div className="divide-y divide-border-subtle">
						{nearData?.members.map((member, idx) => {
							const progressPercent = normalizePercent(member.progressPct);

							return (
								<div
									key={member.channelId}
									className="p-4 hover:bg-linear-to-r hover:from-amber-50/40 hover:to-transparent transition-colors"
								>
									<div className="flex items-center gap-4 mb-3">
										<div className="w-10 h-10 shrink-0 rounded-full bg-linear-to-br from-amber-400 to-amber-500 text-white flex items-center justify-center font-bold shadow-sm shadow-amber-200/50">
											#{idx + 1}
										</div>
										<div className="flex-1 min-w-0">
											<div className="flex justify-between items-start">
												<div className="min-w-0">
													<h4 className="font-bold text-foreground text-lg truncate">
														{member.memberName}
													</h4>
													<div className="text-sm text-muted-foreground flex items-center gap-1">
														<Video size={14} />
														Next: {member.nextMilestone.toLocaleString()}
													</div>
												</div>
												<div className="text-right ml-4 shrink-0">
													<div className="font-mono font-bold text-amber-600 text-lg tabular-nums">
														{progressPercent.toFixed(1)}%
													</div>
													<div className="text-xs text-subtle-foreground tabular-nums">
														{member.remaining.toLocaleString()}명 남음
													</div>
												</div>
											</div>
										</div>
									</div>
									<div className="pl-14">
										<Progress value={progressPercent} className="h-2" />
									</div>
								</div>
							);
						})}
					</div>
				</div>
			</QuerySection>
		</div>
	);
};
