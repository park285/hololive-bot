import Server from "lucide-react/dist/esm/icons/server.mjs";
import { Badge } from "@/components/ui/Badge";
import type { SystemStats } from "@/features/stats/types";
import { cn } from "@/lib/utils";

interface SystemServiceStatusBadgesProps {
	services: SystemStats["serviceRuntime"];
	getServiceColor: (name: string) => string;
}

const formatRuntimeLabel = (
	count: number,
	metricKind: "goroutine" | "thread",
) => `${String(count)} ${metricKind === "thread" ? "threads" : "goroutines"}`;

export const SystemServiceStatusBadges = ({
	services,
	getServiceColor,
}: SystemServiceStatusBadgesProps) => (
	<div className="px-4 py-3 bg-slate-50/50 border-t border-slate-100">
		<div className="flex items-center gap-2 mb-2">
			<Server size={14} className="text-slate-400" />
			<span className="text-xs font-bold text-slate-600 uppercase tracking-wider">
				Service Status
			</span>
		</div>
		<div className="flex gap-2 flex-wrap">
			{services.map((service) => (
				<Badge
					key={service.name}
					variant="outline"
					className="text-[10px] py-0.5 px-2.5 h-6 font-mono bg-white border-slate-200 shadow-sm hover:border-slate-300 transition-colors"
				>
					<span
						className={cn(
							"mr-1.5 h-1.5 w-1.5 rounded-full",
							service.available ? "animate-pulse" : "bg-red-500",
						)}
						style={{
							backgroundColor: service.available
								? getServiceColor(service.name)
								: undefined,
						}}
					/>
					<span
						style={{
							color: service.available
								? getServiceColor(service.name)
								: undefined,
							fontWeight: 600,
						}}
					>
						{service.name}
					</span>
					<span className="text-slate-600 ml-1">
						: {service.available
							? formatRuntimeLabel(service.count, service.metricKind)
							: service.error
								? "ERROR"
								: "OFFLINE"}
					</span>
				</Badge>
			))}
		</div>
	</div>
);
