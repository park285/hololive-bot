import Bot from "lucide-react/dist/esm/icons/bot.mjs";
import Cpu from "lucide-react/dist/esm/icons/cpu.mjs";
import Server from "lucide-react/dist/esm/icons/server.mjs";
import ShieldCheck from "lucide-react/dist/esm/icons/shield-check.mjs";
import type { ServiceStatus } from "@/api/core";

interface ServiceStatusGridProps {
	services: ServiceStatus[];
}

const ServiceIcon = ({ name }: { name: string }) => {
	if (name.includes("hololive"))
		return <Bot className="text-sky-500" size={20} aria-hidden="true" />;
	if (name.includes("llm"))
		return <Cpu className="text-amber-500" size={20} aria-hidden="true" />;
	if (name.includes("admin"))
		return (
			<ShieldCheck className="text-muted-foreground" size={20} aria-hidden="true" />
		);
	return <Server className="text-subtle-foreground" size={20} aria-hidden="true" />;
};

export const ServiceStatusGrid = ({ services }: ServiceStatusGridProps) => (
	<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
		{services.map((service) => (
			<div key={service.name} className="group">
				<div className="bg-card rounded-xl border border-border p-4 shadow-sm hover:shadow-md transition-all duration-200 focus-within:ring-2 focus-within:ring-sky-100">
					<div className="flex items-start justify-between">
						<div className="flex items-center gap-3">
							<div className="p-2.5 bg-muted rounded-xl group-hover:bg-sky-50 transition-colors">
								<ServiceIcon name={service.name} />
							</div>
							<div>
								<h4 className="font-bold text-foreground text-sm">
									{service.name}
								</h4>
								<div
									className="flex items-center gap-1.5 mt-1"
									aria-live="polite"
								>
									{service.available ? (
										<>
											<span
												className="relative flex h-2 w-2"
												aria-hidden="true"
											>
												<span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
												<span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
											</span>
											<span className="text-xs font-bold text-emerald-600">
												온라인
											</span>
										</>
									) : (
										<>
											<div
												className="w-2 h-2 rounded-full bg-rose-500"
												aria-hidden="true"
											/>
											<span className="text-xs font-bold text-rose-600">
												오프라인
											</span>
										</>
									)}
								</div>
							</div>
						</div>

						{service.available && (
							<div className="text-right">
								<div className="text-[10px] uppercase text-subtle-foreground font-bold tracking-wider mb-0.5">
									Response
								</div>
								<div className="text-xs font-mono font-medium text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
									{service.response_time_ms != null
										? `${String(service.response_time_ms)}ms`
										: "-"}
								</div>
							</div>
						)}
					</div>

					{(service.available || service.error) && (
						<div className="mt-4 pt-3 border-t border-border-subtle flex items-center justify-between text-xs">
							<div className="text-muted-foreground font-medium">
								<span className="bg-muted text-muted-foreground px-1.5 py-0.5 rounded text-[10px] font-mono mr-1">
									ERR
								</span>
								{service.error || "none"}
							</div>
							<div className="flex items-center gap-1.5 text-muted-foreground font-medium">
								<Cpu size={14} className="text-subtle-foreground" aria-hidden="true" />
								<span className="font-mono">
									{service.available ? "OK" : "DOWN"}
								</span>
							</div>
						</div>
					)}
				</div>
			</div>
		))}
	</div>
);
