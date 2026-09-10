import { SystemStats as validateSystemStats } from "@/api/generated/validators/system-stats.mjs";
import { CONFIG } from "@/config";
import type { SystemStats } from "@/features/stats/types";

export interface SystemStatsPoint extends SystemStats {
	time: string;
	timestamp: number;
	serviceValues: Record<string, number | null>;
}

export const MAX_DATA_POINTS = 30;
export const CHART_WIDTH = 800;
export const CHART_HEIGHT = 200;
export const CHART_PADDING_X = 24;
export const CHART_PADDING_Y = 18;
export const GOROUTINE_CHART_HEIGHT = 160;

export interface SystemStatsStreamAuthState {
	isAuthenticated: boolean;
	isAuthResolved: boolean;
	isVisible: boolean;
}

const SERVICE_FALLBACK_COLORS = [
	"#0ea5e9",
	"#8b5cf6",
	"#f59e0b",
	"#14b8a6",
	"#ef4444",
	"#6366f1",
];

export const shouldConnectSystemStatsStream = ({
	isAuthenticated,
	isAuthResolved,
	isVisible,
}: SystemStatsStreamAuthState) =>
	isAuthenticated && isAuthResolved && isVisible;

/** parseSystemStats는 현재 세대의 WS 정본만 검증하며 구형 이름·숫자 강제 변환을 허용하지 않습니다. */
export const parseSystemStats = (value: unknown): SystemStats | null => {
	if (!validateSystemStats(value)) return null;
	return value;
};

export const createSystemStatsPoint = (
	stats: SystemStats,
	formattedTime: string,
): SystemStatsPoint => ({
	...stats,
	serviceValues: stats.serviceRuntime.reduce<Record<string, number | null>>(
		(values, service) => {
			values[service.name] = service.available ? service.count : null;
			return values;
		},
		{},
	),
	time: formattedTime,
	timestamp: stats.sampledAt,
});

export const clamp = (value: number, min: number, max: number) =>
	Math.min(max, Math.max(min, value));

export const getServiceColor = (name: string) => {
	const configured = CONFIG.ui.serviceColors[name];
	if (configured) {
		return configured;
	}

	let hash = 0;
	for (const char of name) {
		hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
	}

	return (
		SERVICE_FALLBACK_COLORS[hash % SERVICE_FALLBACK_COLORS.length] ?? "#64748b"
	);
};

export const buildPolylinePath = (
	values: number[],
	maxValue: number,
	width: number,
	height: number,
) => {
	if (values.length === 0) return "";

	const innerWidth = width - CHART_PADDING_X * 2;
	const innerHeight = height - CHART_PADDING_Y * 2;
	const safeMax = Math.max(maxValue, 1);

	return values
		.map((value, index) => {
			const x =
				CHART_PADDING_X +
				(values.length === 1 ? 0.5 : index / (values.length - 1)) * innerWidth;
			const y =
				CHART_PADDING_Y + innerHeight * (1 - clamp(value / safeMax, 0, 1));
			return `${index === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`;
		})
		.join(" ");
};

export const buildAreaPath = (
	values: number[],
	maxValue: number,
	width: number,
	height: number,
) => {
	if (values.length === 0) return "";

	const linePath = buildPolylinePath(values, maxValue, width, height);
	const innerWidth = width - CHART_PADDING_X * 2;
	const baseY = height - CHART_PADDING_Y;
	const lastX = CHART_PADDING_X + innerWidth;

	return `${linePath} L ${lastX.toFixed(2)} ${baseY.toFixed(2)} L ${String(CHART_PADDING_X)} ${baseY.toFixed(2)} Z`;
};

export const getChartLabels = (history: SystemStatsPoint[]) => {
	if (history.length === 0) {
		return [];
	}

	if (history.length === 1) {
		return [
			{
				key: history[0]?.timestamp ?? 0,
				label: history[0]?.time ?? "",
				align: "middle" as const,
				x: "50%",
			},
		];
	}

	const middleIndex = Math.floor((history.length - 1) / 2);

	return [
		{
			key: history[0]?.timestamp ?? 0,
			label: history[0]?.time ?? "",
			align: "start" as const,
			x: "0%",
		},
		{
			key: history[middleIndex]?.timestamp ?? 0,
			label: history[middleIndex]?.time ?? "",
			align: "middle" as const,
			x: "50%",
		},
		{
			key: history[history.length - 1]?.timestamp ?? 0,
			label: history[history.length - 1]?.time ?? "",
			align: "end" as const,
			x: "100%",
		},
	];
};
