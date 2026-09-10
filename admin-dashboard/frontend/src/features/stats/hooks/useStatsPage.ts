import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";
import { queryKeys } from "@/queries/keys";
import { statsApi } from "@/features/stats/api";
import {
	buildCurrentServiceStats,
	buildMainStats,
} from "@/features/stats/selectors";

export function useStatsPage() {
	const navigate = useNavigate();
	const [selectedService, setSelectedService] = useState("hololive-bot");

	const holoQuery = useQuery({
		queryKey: queryKeys.stats.summary,
		queryFn: statsApi.get,
		staleTime: 1000 * 30,
		refetchInterval: 30000,
	});

	const statusQuery = useQuery({
		queryKey: queryKeys.status.aggregated,
		queryFn: statsApi.getStatus,
		staleTime: 1000 * 15,
		refetchInterval: 15000,
	});

	const online = useOnline();
	const holoView = queryView(holoQuery, online, () => false);
	const statusView = queryView(statusQuery, online, data => data.services.length === 0);

	useEffect(() => {
		if (statusQuery.data && statusQuery.data.services.length > 0) {
			setSelectedService((prev) => {
				const exists = statusQuery.data.services.find(
					(service) => service.name === prev,
				);
				if (exists) return prev;
				const defaultService = statusQuery.data.services.find(
					(service) => service.name === "hololive-bot",
				);
				return (
					defaultService?.name ?? statusQuery.data.services[0]?.name ?? prev
				);
			});
		}
	}, [statusQuery.data]);

	const currentServiceStats = useMemo(
		() =>
			buildCurrentServiceStats(
				statusQuery.data,
				holoQuery.data,
				selectedService,
			),
		[statusQuery.data, holoQuery.data, selectedService],
	);

	const mainStats = useMemo(
		() => buildMainStats(holoQuery.data),
		[holoQuery.data],
	);

	return {
		selectedService,
		setSelectedService,
		holoQuery,
		holoView,
		statusView,
		statusQuery,
		currentServiceStats,
		mainStats,
		go: (path: string) => {
			void navigate(path);
		},
	};
}
