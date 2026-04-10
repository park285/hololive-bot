import Bot from "lucide-react/dist/esm/icons/bot";
import ChevronDown from "lucide-react/dist/esm/icons/chevron-down";
import Loader2 from "lucide-react/dist/esm/icons/loader-2";
import Server from "lucide-react/dist/esm/icons/server";
import ShieldCheck from "lucide-react/dist/esm/icons/shield-check";
import { lazy, Suspense } from "react";
import type { AggregatedStatus } from "@/api/core";
import { StatsQuickLinks } from "@/components/dashboard/StatsQuickLinks";

const SystemStatsChart = lazy(() =>
	import("@/components/dashboard/SystemStatsChart").then((m) => ({
		default: m.SystemStatsChart,
	})),
);

const StatsSectionLoader = () => (
	<div className="flex items-center justify-center h-48 text-slate-400 w-full bg-slate-50/50 rounded-lg">
		<Loader2 className="w-6 h-6 animate-spin mr-2" />
		<span className="text-sm">로딩 중…</span>
	</div>
);

export interface CurrentServiceStats {
	name: string;
	available: boolean;
	version: string;
	uptime: string;
}

interface StatsServicesSectionProps {
	statusData?: AggregatedStatus;
	selectedService: string;
	currentServiceStats: CurrentServiceStats;
	onSelectService: (service: string) => void;
	onNavigate: (path: string) => void;
}

const getServiceIcon = (name: string) => {
	if (name.includes("hololive"))
		return <Bot size={20} className="text-sky-500" />;
	if (name.includes("admin"))
		return <ShieldCheck size={20} className="text-slate-500" />;
	return <Server size={20} className="text-slate-400" />;
};

export const StatsServicesSection = ({
	statusData,
	selectedService,
	currentServiceStats,
	onSelectService,
	onNavigate,
}: StatsServicesSectionProps) => (
	<div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
		<div className="lg:col-span-2 space-y-6">
			<div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm animate-fade-in-up stagger-3">
				<div className="flex items-center justify-between mb-4">
					<h3 className="text-lg font-display font-bold text-slate-800 flex items-center gap-2">
						<Server size={20} className="text-slate-500" />
						서비스 상태
					</h3>

					<div className="relative">
						<select
							value={selectedService}
							onChange={(event) => {
								onSelectService(event.target.value);
							}}
							className="appearance-none bg-slate-50 border border-slate-200 text-slate-700 text-sm font-medium rounded-lg py-2 pl-3 pr-8 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:border-transparent cursor-pointer hover:bg-slate-100 transition-colors"
							aria-label="서비스 선택"
						>
							{statusData?.services.map((service) => (
								<option key={service.name} value={service.name}>
									{service.name}
								</option>
							)) || <option value="hololive-bot">hololive-bot</option>}
						</select>
						<ChevronDown
							className="absolute right-2.5 top-2.5 text-slate-400 pointer-events-none"
							size={16}
						/>
					</div>
				</div>

				<div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div className="p-4 bg-slate-50 rounded-xl border border-slate-100 flex items-center justify-between">
						<div>
							<div className="text-xs text-slate-500 font-medium uppercase tracking-wider mb-1">
								Service Status
							</div>
							<div className="flex items-center gap-2">
								{currentServiceStats.available ? (
									<>
										<span className="relative flex h-3 w-3">
											<span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
											<span className="relative inline-flex rounded-full h-3 w-3 bg-emerald-500" />
										</span>
										<span className="font-bold text-slate-700">Online</span>
									</>
								) : (
									<>
										<div className="w-3 h-3 rounded-full bg-rose-500" />
										<span className="font-bold text-slate-700">Offline</span>
									</>
								)}
							</div>
						</div>
						<div className="h-10 w-10 bg-white rounded-full flex items-center justify-center border border-slate-200">
							<ShieldCheck
								size={20}
								className={
									currentServiceStats.available
										? "text-emerald-500"
										: "text-rose-500"
								}
							/>
						</div>
					</div>

					<div className="p-4 bg-slate-50 rounded-xl border border-slate-100 flex items-center justify-between">
						<div>
							<div className="text-xs text-slate-500 font-medium uppercase tracking-wider mb-1">
								Version Info
							</div>
							<div className="font-bold text-slate-700 font-mono text-sm">
								{currentServiceStats.version || "Unknown"}
							</div>
							<div className="text-[10px] text-slate-400 mt-1">
								Uptime: {currentServiceStats.uptime || "-"}
							</div>
						</div>
						<div className="h-10 w-10 bg-white rounded-full flex items-center justify-center border border-slate-200">
							{getServiceIcon(currentServiceStats.name)}
						</div>
					</div>
				</div>
			</div>

			<Suspense fallback={<StatsSectionLoader />}>
				<SystemStatsChart />
			</Suspense>
		</div>

		<StatsQuickLinks onNavigate={onNavigate} />
	</div>
);
