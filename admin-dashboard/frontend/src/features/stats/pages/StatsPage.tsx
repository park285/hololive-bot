import { QueryNotice } from "@/queries/QueryNotice";
import { StatsHero } from "@/features/stats/components/StatsHero";
import { StatsOverviewSection } from "@/features/stats/components/StatsOverviewSection";
import { StatsServicesSection } from "@/features/stats/components/StatsServicesSection";
import { useStatsPage } from "@/features/stats/hooks/useStatsPage";

export const StatsPage = () => {
	const {
		selectedService,
		setSelectedService,
		holoQuery,
		holoView,
		statusView,
		statusQuery,
		currentServiceStats,
		mainStats,
		go,
	} = useStatsPage();


	return (
		<div className="space-y-8">
			<StatsHero />
			<QueryNotice view={holoView} label="Hololive 통계" onRetry={() => { void holoQuery.refetch(); }} />
			{holoView.data !== undefined && <StatsOverviewSection cards={mainStats} />}
			<QueryNotice view={statusView} label="서비스 상태" onRetry={() => { void statusQuery.refetch(); }} />
			<StatsServicesSection
				statusData={statusQuery.data}
				selectedService={selectedService}
				currentServiceStats={currentServiceStats}
				onSelectService={setSelectedService}
				onNavigate={go}
			/>
		</div>
	);
};
