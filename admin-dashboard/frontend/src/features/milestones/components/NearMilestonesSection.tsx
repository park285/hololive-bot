import TrendingUp from "lucide-react/dist/esm/icons/trending-up.mjs";
import Video from "lucide-react/dist/esm/icons/video.mjs";
import { Progress } from "@/components/ui/Progress";
import type { NearMilestonesResponse } from "@/features/milestones/types";

interface NearMilestonesSectionProps {
	nearData?: NearMilestonesResponse;
}

const normalizePercent = (value: number) => {
	const percent = value <= 1 ? value * 100 : value;
	return Math.min(100, Math.max(0, percent));
};

export const NearMilestonesSection = ({
	nearData,
}: NearMilestonesSectionProps) => (
	<div className="space-y-4">
		<div className="flex items-center justify-between pb-2 border-b border-slate-200">
			<h3 className="text-lg font-bold text-slate-800 flex items-center gap-2">
				<TrendingUp size={20} className="text-amber-500" />
				{nearData?.threshold && nearData.threshold > 0
					? "달성 임박 멤버"
					: "달성 근접 멤버"}
				{nearData?.threshold && nearData.threshold > 0 && (
					<span className="ml-2 text-xs py-1 px-2 bg-amber-50 text-amber-600 rounded-full font-medium">
						진행률 {normalizePercent(nearData.threshold).toFixed(0)}% 이상
					</span>
				)}
			</h3>
			<span className="text-slate-500 text-sm font-medium">
				{nearData?.count ?? 0}명
			</span>
		</div>

		<div className="bg-white rounded-2xl border border-slate-200 shadow-sm overflow-hidden">
			{(nearData?.members.length ?? 0) === 0 ? (
				<div className="text-center py-12 text-slate-500">
					현재 달성 임박 멤버가 없습니다.
				</div>
			) : (
				<div className="divide-y divide-slate-100">
					{nearData?.members.map((member, idx) => {
						const progressPercent = normalizePercent(member.progressPct);

						return (
							<div
								key={member.channelId}
								className="p-4 hover:bg-slate-50 transition-colors"
							>
								<div className="flex items-center gap-4 mb-3">
									<div className="w-10 h-10 shrink-0 rounded-full bg-amber-50 text-amber-600 flex items-center justify-center font-bold">
										#{idx + 1}
									</div>
									<div className="flex-1 min-w-0">
										<div className="flex justify-between items-start">
											<div>
												<h4 className="font-bold text-slate-800 text-lg truncate">
													{member.memberName}
												</h4>
												<div className="text-sm text-slate-500 flex items-center gap-1">
													<Video size={14} />
													Next: {member.nextMilestone.toLocaleString()}
												</div>
											</div>
											<div className="text-right ml-4 shrink-0">
												<div className="font-mono font-bold text-amber-600 text-lg tabular-nums">
													{progressPercent.toFixed(1)}%
												</div>
												<div className="text-xs text-slate-400 tabular-nums">
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
			)}
		</div>
	</div>
);
