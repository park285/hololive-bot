import { cn } from "@/lib/utils";
import {
	CHART_PADDING_X,
	CHART_PADDING_Y,
	CHART_WIDTH,
	GOROUTINE_CHART_HEIGHT,
	getChartLabels,
	getServiceColor,
	type SystemStatsPoint,
} from "../lib/systemStats";

interface RuntimeUnitsChartProps {
	history: SystemStatsPoint[];
	serviceNames: string[];
}

export const RuntimeUnitsChart = ({
	history,
	serviceNames,
}: RuntimeUnitsChartProps) => {
	const maxValue = Math.max(1, ...history.map((point) => point.totalRuntimeUnits));
	const labels = getChartLabels(history);
	const innerHeight = GOROUTINE_CHART_HEIGHT - CHART_PADDING_Y * 2;
	const innerWidth = CHART_WIDTH - CHART_PADDING_X * 2;
	const columnWidth =
		history.length > 0 ? innerWidth / history.length : innerWidth;
	const barWidth = Math.max(6, columnWidth - 4);

	return (
		<div className="w-full">
			<div className="relative h-[160px] w-full overflow-hidden rounded-lg border border-slate-100 bg-white">
				<svg
					viewBox={`0 0 ${String(CHART_WIDTH)} ${String(GOROUTINE_CHART_HEIGHT)}`}
					className="h-full w-full"
					preserveAspectRatio="none"
					aria-label="서비스별 런타임 단위 추이"
				>
					{[0, 0.5, 1].map((ratio) => {
						const y = CHART_PADDING_Y + innerHeight * ratio;
						const labelValue = Math.round(maxValue * (1 - ratio));
						return (
							<g key={ratio}>
								<line
									x1={CHART_PADDING_X}
									y1={y}
									x2={CHART_WIDTH - CHART_PADDING_X}
									y2={y}
									stroke="#e2e8f0"
									strokeDasharray="4 4"
								/>
								<text x={6} y={y + 4} fill="#94a3b8" fontSize="10">
									{labelValue}
								</text>
							</g>
						);
					})}

					{history.map((point, pointIndex) => {
						const x =
							CHART_PADDING_X +
							pointIndex * columnWidth +
							Math.max((columnWidth - barWidth) / 2, 1);
						let stackOffset = 0;

						return serviceNames.map((serviceName) => {
							const value = point.serviceValues[serviceName] ?? 0;
							if (value <= 0) {
								return null;
							}

							const height = (value / maxValue) * innerHeight;
							const y =
								GOROUTINE_CHART_HEIGHT - CHART_PADDING_Y - stackOffset - height;
							stackOffset += height;

							return (
								<rect
									key={`${String(point.timestamp)}-${serviceName}`}
									x={x}
									y={y}
									width={barWidth}
									height={height}
									rx={Math.min(3, barWidth / 3)}
									fill={getServiceColor(serviceName)}
									opacity="0.88"
								>
									<title>{`${serviceName}: ${String(value)} (${point.time})`}</title>
								</rect>
							);
						});
					})}
				</svg>
			</div>

			<div className="mt-3 flex items-center justify-between font-mono text-[11px] text-slate-400">
				{labels.map((label) => (
					<span
						key={label.key}
						className={cn(
							label.align === "start" && "text-left",
							label.align === "middle" && "text-center",
							label.align === "end" && "text-right",
						)}
						style={{ width: "33%" }}
					>
						{label.label}
					</span>
				))}
			</div>
		</div>
	);
};
