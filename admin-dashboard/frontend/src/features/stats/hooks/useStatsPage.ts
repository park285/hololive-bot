import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { statusApi } from "@/api/core";
import { queryKeys } from "@/api/queryKeys";
import { statsApi } from "@/features/stats/api";
import {
	buildCurrentServiceStats,
	buildMainStats,
} from "@/features/stats/selectors";
import { visibleRefetchInterval } from "@/lib/polling";

export function useStatsPage() {
	const navigate = useNavigate();
	const [selectedService, setSelectedService] = useState("hololive-bot");

	const holoQuery = useQuery({
		queryKey: queryKeys.stats.summary,
		queryFn: statsApi.get,
		staleTime: 1000 * 30,
		refetchInterval: visibleRefetchInterval(30000),
	});

	const statusQuery = useQuery({
		queryKey: queryKeys.status.aggregated,
		queryFn: statusApi.get,
		staleTime: 1000 * 15,
		refetchInterval: visibleRefetchInterval(15000),
	});

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
		statusQuery,
		currentServiceStats,
		mainStats,
		go: (path: string) => {
			void navigate(path);
		},
	};
}
