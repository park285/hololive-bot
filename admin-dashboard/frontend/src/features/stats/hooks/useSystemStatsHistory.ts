import { startTransition, useEffect, useMemo, useState } from "react";
import { useWebSocket } from "@/queries/useWebSocket";
import { generation, verifyMetadata, session } from "@/app/bootstrap";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { useSessionSnapshot } from "@/session/useSession";
import type { SystemStats } from "@/features/stats/types";
import {
	MAX_DATA_POINTS,
	type SystemStatsPoint,
	createSystemStatsPoint,
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
	const [invalidSample, setInvalidSample] = useState(false);
	const [isVisible, setIsVisible] = useState(
		() => typeof document === "undefined" || document.visibilityState === "visible",
	);
	const auth = useSessionSnapshot();
	const isAuthenticated = auth.phase === "authenticated";
	const isAuthResolved = auth.phase !== "pending";

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
		protocol: `admin-stats.${CLIENT_GENERATION}`,
		beforeConnect: verifyMetadata,
		canConnect: () => generation.snapshot().phase === "ready" && session.state.snapshot().phase === "authenticated",
		autoConnect: shouldConnect,
		parseMessage: (data) => {
			const parsed = parseSystemStats(data);
			setInvalidSample(parsed === null);
			return parsed;
		},
		onMessage: (data) => {
			const timeStr = systemStatsTimeFormatter.format(new Date(data.sampledAt));
			const point: SystemStatsPoint = createSystemStatsPoint(data, timeStr);

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
		invalidSample,
		latestPoint,
		serviceNames,
		statsHistory,
	};
};
