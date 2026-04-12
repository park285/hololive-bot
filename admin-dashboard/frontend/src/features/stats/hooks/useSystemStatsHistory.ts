import { useMemo, useState } from "react";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useAuthStore } from "@/stores/authStore";
import type { SystemStats } from "@/features/stats/types";
import {
	MAX_DATA_POINTS,
	type SystemStatsPoint,
	parseSystemStats,
	shouldConnectSystemStatsStream,
} from "../lib/systemStats";

export const useSystemStatsHistory = () => {
	const [statsHistory, setStatsHistory] = useState<SystemStatsPoint[]>([]);
	const [currentStats, setCurrentStats] = useState<SystemStats | null>(null);
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const isAuthResolved = useAuthStore((state) => state.isAuthResolved);

	const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
	const wsUrl = `${protocol}//${window.location.host}/admin/api/ws/system-stats`;
	const shouldConnect = shouldConnectSystemStatsStream({
		isAuthenticated,
		isAuthResolved,
	});

	const { isConnected } = useWebSocket<SystemStats>(wsUrl, {
		autoConnect: shouldConnect,
		parseMessage: (data) => parseSystemStats(data),
		onMessage: (data) => {
			const now = new Date();
			const timeStr = now.toLocaleTimeString("ko-KR", {
				hour12: false,
				hour: "2-digit",
				minute: "2-digit",
				second: "2-digit",
			});

			const serviceValues = data.serviceRuntime.reduce<Record<string, number>>(
				(acc, service) => {
					acc[service.name] = service.available ? service.count : 0;
					return acc;
				},
				{},
			);

			const point: SystemStatsPoint = {
				...data,
				serviceValues,
				time: timeStr,
				timestamp: now.getTime(),
			};

			setCurrentStats(data);
			setStatsHistory((prev) => [...prev, point].slice(-MAX_DATA_POINTS));
		},
		reconnectInterval: 5000,
	});

	const serviceNames = useMemo(() => {
		const names = new Set<string>();
		statsHistory.forEach((point) => {
			Object.keys(point.serviceValues).forEach((name) => {
				names.add(name);
			});
		});
		currentStats?.serviceRuntime.forEach((service) => {
			names.add(service.name);
		});
		return [...names];
	}, [currentStats, statsHistory]);

	const latestPoint = statsHistory[statsHistory.length - 1];

	return {
		currentStats,
		isConnected,
		latestPoint,
		serviceNames,
		statsHistory,
	};
};
