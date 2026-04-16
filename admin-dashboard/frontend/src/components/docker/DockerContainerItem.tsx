import clsx from "clsx";
import AlertCircle from "lucide-react/dist/esm/icons/alert-circle";
import Play from "lucide-react/dist/esm/icons/play";
import Power from "lucide-react/dist/esm/icons/power";
import RefreshCw from "lucide-react/dist/esm/icons/refresh-cw";
import Square from "lucide-react/dist/esm/icons/square";
import StopCircle from "lucide-react/dist/esm/icons/stop-circle";
import type { DockerContainer } from "@/api/core";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";

interface DockerContainerItemProps {
	container: DockerContainer;
	actionInProgress: string | null;
	onAction: (
		containerName: string,
		action: "restart" | "stop" | "start",
	) => void;
}

export const DockerContainerItem = ({
	container,
	actionInProgress,
	onAction,
}: DockerContainerItemProps) => {
	const isActionPending = actionInProgress === container.name;

	return (
		<div className="group flex flex-col sm:flex-row items-start sm:items-center gap-4 p-4 bg-slate-50 rounded-xl border border-slate-100 hover:bg-white hover:shadow-md hover:border-slate-200 transition-all duration-200 focus-within:ring-2 focus-within:ring-sky-100">
			<div
				className={clsx(
					"w-12 h-12 rounded-xl border flex items-center justify-center shrink-0 shadow-sm transition-colors",
					container.state === "running"
						? "bg-white border-slate-100 text-sky-500"
						: "bg-slate-100 border-slate-200 text-slate-400",
				)}
				aria-hidden="true"
			>
				{container.state === "running" ? (
					container.health === "unhealthy" ? (
						<AlertCircle size={20} className="text-amber-500" />
					) : (
						<Play size={20} className="fill-current" />
					)
				) : (
					<Square size={20} className="fill-current" />
				)}
			</div>

			<div className="flex-1 min-w-0 flex flex-col gap-1 w-full">
				<div className="flex items-center justify-between sm:justify-start gap-2">
					<span className="font-bold text-slate-800 text-base truncate">
						{container.name}
					</span>
					{container.managed && <Badge color="sky">관리됨</Badge>}
				</div>

				<div className="flex items-center gap-2 text-xs flex-wrap">
					<Badge
						color={container.state === "running" ? "green" : "gray"}
						className="uppercase tracking-wider font-bold"
					>
						{container.state}
					</Badge>

					{container.health && container.health !== "none" && (
						<Badge
							color={container.health === "healthy" ? "sky" : "amber"}
							className="uppercase tracking-wider font-bold"
						>
							{container.health}
						</Badge>
					)}

					<span
						className="hidden sm:inline text-slate-300 pointer-events-none shrink-0"
						aria-hidden="true"
					>
						•
					</span>

					<span
						className="font-mono text-slate-400 bg-slate-100 px-1.5 py-0.5 rounded text-[10px] truncate max-w-[200px]"
						title={container.image}
					>
						{container.image.split(":")[0]?.split("/").pop() ?? "unknown"}
					</span>
				</div>
			</div>

			<div className="shrink-0 flex gap-2 w-full sm:w-auto mt-2 sm:mt-0 justify-end">
				{container.state === "running" ? (
					<>
						<Button
							size="sm"
							variant="secondary"
							onClick={() => {
								onAction(container.name, "restart");
							}}
							disabled={isActionPending}
							className={clsx(
								"h-9 px-3 gap-1.5 font-bold hover:bg-amber-50 hover:text-amber-600 hover:border-amber-200 focus-visible:ring-2 focus-visible:ring-amber-200",
								isActionPending && "cursor-wait opacity-70",
							)}
							title="재시작"
							aria-label={`${container.name} 컨테이너 재시작`}
						>
							{isActionPending ? (
								<RefreshCw
									size={14}
									className="animate-spin"
									aria-hidden="true"
								/>
							) : (
								<Power size={14} aria-hidden="true" />
							)}
							<span className="sm:hidden lg:inline">재시작</span>
						</Button>
						<Button
							size="sm"
							variant="secondary"
							onClick={() => {
								onAction(container.name, "stop");
							}}
							disabled={isActionPending}
							className="h-9 px-3 gap-1.5 font-bold hover:bg-rose-50 hover:text-rose-600 hover:border-rose-200 focus-visible:ring-2 focus-visible:ring-rose-200"
							title="중지"
							aria-label={`${container.name} 컨테이너 중지`}
						>
							<StopCircle size={14} aria-hidden="true" />
							<span className="sm:hidden lg:inline">중지</span>
						</Button>
					</>
				) : (
					<Button
						size="sm"
						variant="secondary"
						onClick={() => {
							onAction(container.name, "start");
						}}
						disabled={isActionPending}
						className="h-9 px-3 gap-1.5 font-bold hover:bg-emerald-50 hover:text-emerald-600 hover:border-emerald-200 focus-visible:ring-2 focus-visible:ring-emerald-200"
						title="시작"
						aria-label={`${container.name} 컨테이너 시작`}
					>
						<Play size={14} aria-hidden="true" />
						<span className="sm:hidden lg:inline">시작</span>
					</Button>
				)}
			</div>
		</div>
	);
};
