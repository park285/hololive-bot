import type { ReactNode } from "react";
import { StatCard } from "@/components/ui/StatCard";

export interface MilestonesStatCard {
	label: string;
	value: number;
	variant: "indigo" | "yellow" | "green" | "rose";
	icon: ReactNode;
}

interface MilestonesStatsSectionProps {
	cards: MilestonesStatCard[];
}

export const MilestonesStatsSection = ({
	cards,
}: MilestonesStatsSectionProps) => (
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
);
