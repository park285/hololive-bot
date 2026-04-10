import Activity from "lucide-react/dist/esm/icons/activity";
import type { ReactNode } from "react";
import { StatCard } from "@/components/ui/StatCard";

export interface StatsOverviewCard {
	label: string;
	value: number;
	variant: "cyan" | "rose" | "indigo";
	icon: ReactNode;
}

interface StatsOverviewSectionProps {
	cards: StatsOverviewCard[];
}

export const StatsOverviewSection = ({ cards }: StatsOverviewSectionProps) => (
	<div>
		<h3 className="text-lg font-display font-bold text-slate-800 mb-4 flex items-center gap-2">
			<Activity size={20} className="text-sky-500" />
			실시간 현황 (Hololive Bot)
		</h3>
		<div className="grid grid-cols-1 md:grid-cols-3 gap-6">
			{cards.map((card, idx) => (
				<div
					key={card.label}
					className="animate-fade-in-up"
					style={{ animationDelay: `${String((idx + 1) * 80)}ms` }}
				>
					<StatCard
						label={card.label}
						value={card.value}
						icon={card.icon}
						variant={card.variant}
					/>
				</div>
			))}
		</div>
	</div>
);
