import Activity from "lucide-react/dist/esm/icons/activity";
import CircuitBoard from "lucide-react/dist/esm/icons/circuit-board";
import Cpu from "lucide-react/dist/esm/icons/cpu";
import Layers from "lucide-react/dist/esm/icons/layers";
import { SystemServiceStatusBadges } from "@/components/dashboard/SystemServiceStatusBadges";
import { Card } from "@/components/ui/Card";
import { ChartSkeleton } from "@/features/stats/components/ChartSkeleton";
import { GoroutineChart } from "@/features/stats/components/GoroutineChart";
import { ResourceChart } from "@/features/stats/components/ResourceChart";
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
			<Card.Header className="flex flex-row items-center justify-between border-b border-slate-100 bg-slate-50/50 pb-4">
				<div className="flex items-center gap-2">
					<Activity className="text-slate-500" size={20} />
					<h3 className="text-lg font-display font-bold text-slate-800">
						시스템 리소스
					</h3>
					{isConnected ? (
						<span className="relative ml-2 flex h-2 w-2">
							<span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
							<span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
						</span>
					) : (
						<span className="ml-2 h-2 w-2 rounded-full bg-slate-300" />
					)}
				</div>

				{currentStats && (
					<div className="flex gap-4 font-mono text-xs">
						<div className="flex items-center gap-1.5 rounded border border-slate-100 bg-white px-2 py-1 shadow-sm">
							<Cpu size={14} className="text-sky-500" />
							<span className="font-bold text-slate-700">
								{currentStats.cpuUsage.toFixed(1)}%
							</span>
						</div>
						<div className="flex items-center gap-1.5 rounded border border-slate-100 bg-white px-2 py-1 shadow-sm">
							<Layers size={14} className="text-violet-500" />
							<span className="font-bold text-slate-700">
								{currentStats.memoryUsage.toFixed(1)}%
							</span>
						</div>
						<div className="hidden items-center gap-1.5 rounded border border-slate-100 bg-white px-2 py-1 shadow-sm sm:flex">
							<CircuitBoard size={14} className="text-slate-400" />
							<span className="font-bold text-slate-500">
								{currentStats.totalGoroutines} Goroutines
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

				<div className="border-t border-slate-100 px-4 py-3">
					<div className="mb-4 flex items-center justify-between gap-3">
						<div className="flex items-center gap-2">
							<CircuitBoard size={16} className="text-slate-400" />
							<h4 className="text-sm font-bold text-slate-700">
								서비스별 고루틴
							</h4>
						</div>
						{latestPoint && (
							<span className="font-mono text-[11px] text-slate-400">
								latest {latestPoint.time}
							</span>
						)}
					</div>

					<GoroutineChart history={statsHistory} serviceNames={serviceNames} />
				</div>

				{currentStats && (
					<SystemServiceStatusBadges
						services={currentStats.serviceGoroutines}
						getServiceColor={getServiceColor}
					/>
				)}
			</Card.Body>
		</Card>
	);
};
