import Activity from "lucide-react/dist/esm/icons/activity.mjs";
import CircuitBoard from "lucide-react/dist/esm/icons/circuit-board.mjs";
import Cpu from "lucide-react/dist/esm/icons/cpu.mjs";
import Layers from "lucide-react/dist/esm/icons/layers.mjs";
import { SystemServiceStatusBadges } from "@/components/dashboard/SystemServiceStatusBadges";
import { Card } from "@/components/ui/Card";
import { ChartSkeleton } from "@/features/stats/components/ChartSkeleton";
import { ResourceChart } from "@/features/stats/components/ResourceChart";
import { RuntimeUnitsChart } from "@/features/stats/components/RuntimeUnitsChart";
import { useSystemStatsHistory } from "@/features/stats/hooks/useSystemStatsHistory";
import { getServiceColor } from "@/features/stats/lib/systemStats";

export const SystemStatsChart = () => {
	const {
		currentStats,
		isConnected,
		latestPoint,
		serviceNames,
		statsHistory,
	} = useSystemStatsHistory();

	return (
		<Card className="overflow-hidden">
			<Card.Header className="flex flex-row flex-wrap items-center justify-between gap-2 border-b border-border-subtle bg-muted/50 pb-4">
				<div className="flex items-center gap-2">
					<Activity className="text-muted-foreground" size={20} />
					<h3 className="text-lg font-display font-bold text-foreground">
						시스템 리소스
					</h3>
					{isConnected ? (
						<span className="relative ml-2 flex h-2 w-2">
							<span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
							<span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
						</span>
					) : (
						<span className="ml-2 h-2 w-2 rounded-full bg-slate-300 dark:bg-slate-600" />
					)}
				</div>

				{currentStats && (
					<div className="flex gap-4 font-mono text-xs">
						<div className="flex items-center gap-1.5 rounded border border-border-subtle bg-card px-2 py-1 shadow-sm">
							<Cpu size={14} className="text-sky-500" />
							<span className="font-bold text-foreground">
								{currentStats.cpuUsage.toFixed(1)}%
							</span>
						</div>
						<div className="flex items-center gap-1.5 rounded border border-border-subtle bg-card px-2 py-1 shadow-sm">
							<Layers size={14} className="text-violet-500" />
							<span className="font-bold text-foreground">
								{currentStats.memoryUsage.toFixed(1)}%
							</span>
						</div>
						<div className="hidden items-center gap-1.5 rounded border border-border-subtle bg-card px-2 py-1 shadow-sm sm:flex">
							<CircuitBoard size={14} className="text-subtle-foreground" />
							<span className="font-bold text-muted-foreground">
								Go {currentStats.totalGoGoroutines}
							</span>
						</div>
						<div className="hidden items-center gap-1.5 rounded border border-border-subtle bg-card px-2 py-1 shadow-sm sm:flex">
							<CircuitBoard size={14} className="text-subtle-foreground" />
							<span className="font-bold text-muted-foreground">
								Threads {currentStats.threadCount}
							</span>
						</div>
					</div>
				)}
			</Card.Header>

			<Card.Body className="relative p-0">
				{statsHistory.length < 2 && <ChartSkeleton label="데이터 수집 중…" />}

				<div className="p-4">
					<ResourceChart history={statsHistory} />
				</div>

				<div className="border-t border-border-subtle px-4 py-3">
					<div className="mb-4 flex items-center justify-between gap-3">
						<div className="flex items-center gap-2">
							<CircuitBoard size={16} className="text-subtle-foreground" />
							<h4 className="text-sm font-bold text-foreground">
								서비스별 런타임 단위
							</h4>
						</div>
						{latestPoint && (
							<span className="font-mono text-[11px] text-subtle-foreground">
								latest {latestPoint.time}
							</span>
						)}
					</div>

					<RuntimeUnitsChart history={statsHistory} serviceNames={serviceNames} />
				</div>

				{currentStats && (
					<SystemServiceStatusBadges
						services={currentStats.serviceRuntime}
						getServiceColor={getServiceColor}
					/>
				)}
			</Card.Body>
		</Card>
	);
};
