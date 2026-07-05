import { cn } from "@/lib/utils";
import {
	CHART_HEIGHT,
	CHART_PADDING_X,
	CHART_PADDING_Y,
	CHART_WIDTH,
	buildAreaPath,
	buildPolylinePath,
	getChartLabels,
	type SystemStatsPoint,
} from "../lib/systemStats";

interface ResourceChartProps {
	history: SystemStatsPoint[];
}

export const ResourceChart = ({ history }: ResourceChartProps) => {
	const cpuValues = history.map((point) => point.cpuUsage);
	const memoryValues = history.map((point) => point.memoryUsage);
	const maxValue = Math.max(100, ...cpuValues, ...memoryValues);
	const labels = getChartLabels(history);

	const cpuAreaPath = buildAreaPath(
		cpuValues,
		maxValue,
		CHART_WIDTH,
		CHART_HEIGHT,
	);
	const memoryAreaPath = buildAreaPath(
		memoryValues,
		maxValue,
		CHART_WIDTH,
		CHART_HEIGHT,
	);
	const cpuLinePath = buildPolylinePath(
		cpuValues,
		maxValue,
		CHART_WIDTH,
		CHART_HEIGHT,
	);
	const memoryLinePath = buildPolylinePath(
		memoryValues,
		maxValue,
		CHART_WIDTH,
		CHART_HEIGHT,
	);

	return (
		<div className="w-full">
			<div className="relative h-[200px] w-full overflow-hidden rounded-lg border border-border-subtle bg-card">
				<svg
					viewBox={`0 0 ${String(CHART_WIDTH)} ${String(CHART_HEIGHT)}`}
					className="h-full w-full"
					preserveAspectRatio="none"
					aria-label="CPU 및 메모리 사용량 추이"
				>
					<defs>
						<linearGradient id="cpuGradient" x1="0" y1="0" x2="0" y2="1">
							<stop
								offset="0%"
								style={{ stopColor: "hsl(var(--chart-cpu))" }}
								stopOpacity="0.28"
							/>
							<stop
								offset="100%"
								style={{ stopColor: "hsl(var(--chart-cpu))" }}
								stopOpacity="0.02"
							/>
						</linearGradient>
						<linearGradient id="memoryGradient" x1="0" y1="0" x2="0" y2="1">
							<stop
								offset="0%"
								style={{ stopColor: "hsl(var(--chart-memory))" }}
								stopOpacity="0.22"
							/>
							<stop
								offset="100%"
								style={{ stopColor: "hsl(var(--chart-memory))" }}
								stopOpacity="0.02"
							/>
						</linearGradient>
					</defs>

					{[0, 25, 50, 75, 100].map((tick) => {
						const y =
							CHART_PADDING_Y +
							(CHART_HEIGHT - CHART_PADDING_Y * 2) * (1 - tick / 100);
						return (
							<g key={tick}>
								<line
									x1={CHART_PADDING_X}
									y1={y}
									x2={CHART_WIDTH - CHART_PADDING_X}
									y2={y}
									style={{ stroke: "hsl(var(--chart-grid))" }}
									strokeDasharray="4 4"
								/>
								<text
									x={6}
									y={y + 4}
									style={{ fill: "hsl(var(--chart-axis))" }}
									fontSize="10"
								>
									{tick}%
								</text>
							</g>
						);
					})}

					{memoryAreaPath && (
						<path d={memoryAreaPath} fill="url(#memoryGradient)" />
					)}
					{cpuAreaPath && <path d={cpuAreaPath} fill="url(#cpuGradient)" />}

					{memoryLinePath && (
						<path
							d={memoryLinePath}
							fill="none"
							style={{ stroke: "hsl(var(--chart-memory))" }}
							strokeWidth="3"
							strokeLinejoin="round"
							strokeLinecap="round"
						/>
					)}
					{cpuLinePath && (
						<path
							d={cpuLinePath}
							fill="none"
							style={{ stroke: "hsl(var(--chart-cpu))" }}
							strokeWidth="3"
							strokeLinejoin="round"
							strokeLinecap="round"
						/>
					)}
				</svg>
			</div>

			<div className="mt-3 flex items-center justify-between font-mono text-[11px] text-subtle-foreground">
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

			<div className="mt-3 flex flex-wrap gap-3 text-xs">
				<div className="inline-flex items-center gap-2 rounded-full bg-sky-50 px-3 py-1 font-medium text-sky-700 dark:bg-sky-950/40 dark:text-sky-300">
					<span className="h-2 w-2 rounded-full bg-sky-500" />
					CPU
				</div>
				<div className="inline-flex items-center gap-2 rounded-full bg-violet-50 px-3 py-1 font-medium text-violet-700 dark:bg-violet-950/40 dark:text-violet-300">
					<span className="h-2 w-2 rounded-full bg-violet-500" />
					Memory
				</div>
			</div>
		</div>
	);
};
