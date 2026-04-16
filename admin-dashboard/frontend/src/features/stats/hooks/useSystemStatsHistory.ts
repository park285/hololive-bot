import { startTransition, useEffect, useMemo, useState } from "react";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useAuthStore } from "@/stores/authStore";
import type { SystemStats } from "@/features/stats/types";
import {
	MAX_DATA_POINTS,
	type SystemStatsPoint,
	parseSystemStats,
	shouldConnectSystemStatsStream,
} from "../lib/systemStats";

const systemStatsTimeFormatter = new Intl.DateTimeFormat("ko-KR", {
	hour12: false,
	hour: "2-digit",
	minute: "2-digit",
	second: "2-digit",
});

export const useSystemStatsHistory = () => {
	const [statsHistory, setStatsHistory] = useState<SystemStatsPoint[]>([]);
	const [currentStats, setCurrentStats] = useState<SystemStats | null>(null);
	const [isVisible, setIsVisible] = useState(
		() => typeof document === "undefined" || document.visibilityState === "visible",
	);
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const isAuthResolved = useAuthStore((state) => state.isAuthResolved);

	useEffect(() => {
		if (typeof document === "undefined") {
			return;
		}

		const handleVisibilityChange = () => {
			setIsVisible(document.visibilityState === "visible");
		};

		document.addEventListener("visibilitychange", handleVisibilityChange);
		return () => {
			document.removeEventListener("visibilitychange", handleVisibilityChange);
		};
	}, []);

	const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
	const wsUrl = `${protocol}//${window.location.host}/admin/api/ws/system-stats`;
	const shouldConnect = shouldConnectSystemStatsStream({
		isAuthenticated,
		isAuthResolved,
		isVisible,
	});

	const { isConnected } = useWebSocket<SystemStats>(wsUrl, {
		autoConnect: shouldConnect,
		enablePing: false,
		parseMessage: (data) => parseSystemStats(data),
		onMessage: (data) => {
			const now = new Date();
			const timeStr = systemStatsTimeFormatter.format(now);

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

			startTransition(() => {
				setCurrentStats(data);
				setStatsHistory((prev) => [...prev, point].slice(-MAX_DATA_POINTS));
			});
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
