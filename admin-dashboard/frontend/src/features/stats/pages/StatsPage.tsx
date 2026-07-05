import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import { Button } from "@/components/ui/Button";
import { StatsHero } from "@/features/stats/components/StatsHero";
import { StatsOverviewSection } from "@/features/stats/components/StatsOverviewSection";
import { StatsServicesSection } from "@/features/stats/components/StatsServicesSection";
import { useStatsPage } from "@/features/stats/hooks/useStatsPage";

export const StatsPage = () => {
	const {
		selectedService,
		setSelectedService,
		holoQuery,
		statusQuery,
		currentServiceStats,
		mainStats,
		go,
	} = useStatsPage();

	if (holoQuery.isLoading && statusQuery.isLoading) {
		return (
			<div className="flex justify-center items-center h-64 text-subtle-foreground">
				<div className="animate-spin mr-2">
					<Loader2 />
				</div>
				데이터를 불러오는 중…
			</div>
		);
	}

	if (holoQuery.isError && statusQuery.isError) {
		return (
			<div className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100">
				<div className="text-rose-600 font-bold mb-2">
					통계를 불러올 수 없습니다
				</div>
				<Button
					onClick={() => {
						void holoQuery.refetch();
						void statusQuery.refetch();
					}}
					className="bg-rose-600 hover:bg-rose-700 text-white"
				>
					다시 시도
				</Button>
			</div>
		);
	}

	return (
		<div className="space-y-8">
			<StatsHero />
			<StatsOverviewSection cards={mainStats} />
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
