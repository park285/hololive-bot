import { AchievedMilestonesSection } from "@/features/milestones/components/AchievedMilestonesSection";
import { MilestonesStatsSection } from "@/features/milestones/components/MilestonesStatsSection";
import { NearMilestonesSection } from "@/features/milestones/components/NearMilestonesSection";

export const MilestonesPage = () => (
	<div className="space-y-8">
		<div className="flex items-center gap-4 mb-2">
			<div className="w-1 h-12 rounded-full bg-linear-to-b from-indigo-400 to-cyan-400 shrink-0" />
			<div className="flex flex-col gap-1">
				<h2 className="text-2xl font-display font-bold text-foreground tracking-tight">
					Milestone Tracker
				</h2>
				<p className="text-muted-foreground">
					구독자 마일스톤 달성 현황 및 임박 멤버 모니터링
				</p>
			</div>
		</div>

		<MilestonesStatsSection />

		<div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
			<NearMilestonesSection />
			<AchievedMilestonesSection />
		</div>
	</div>
);
