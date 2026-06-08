import clsx from "clsx";
import AlertCircle from "lucide-react/dist/esm/icons/alert-circle.mjs";
import RefreshCw from "lucide-react/dist/esm/icons/refresh-cw.mjs";
import Server from "lucide-react/dist/esm/icons/server.mjs";
import type { DockerContainer } from "@/api/core";
import { useDockerContainerActions } from "@/components/settings/DockerContainerActions";
import { DockerContainerConfirmModal } from "@/components/settings/DockerContainerConfirmModal";
import { DockerContainerItem } from "@/components/docker/DockerContainerItem";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { VirtualList } from "@/components/ui/VirtualList";

interface DockerContainerListProps {
	initialHealth?: { status: string; available: boolean };
	initialContainers?: { status: string; containers: DockerContainer[] };
}

export const DockerContainerList = ({
	initialHealth,
	initialContainers,
}: DockerContainerListProps) => {
	const {
		dockerHealth,
		containers,
		containersLoading,
		containersRefetching,
		isManualRefetching,
		actionInProgress,
		confirmModal,
		openConfirmModal,
		closeConfirmModal,
		handleConfirmAction,
		handleRefresh,
	} = useDockerContainerActions({ initialHealth, initialContainers });

	return (
		<>
			<Card>
				<Card.Header className="flex flex-row items-center justify-between border-b border-slate-100 pb-4">
					<div className="flex items-center gap-2">
						<Server className="text-slate-600" size={20} />
						<h3 className="text-lg font-bold text-slate-800">컨테이너 관리</h3>
						{dockerHealth?.available ? (
							<Badge color="green" className="px-2 py-0.5">
								Docker 연결됨
							</Badge>
						) : (
							<Badge color="rose" className="px-2 py-0.5">
								Docker 연결 안됨
							</Badge>
						)}
					</div>
					<Button
						variant="ghost"
						size="icon"
						onClick={() => {
							void handleRefresh();
						}}
						disabled={!dockerHealth?.available || containersLoading || isManualRefetching}
						className={clsx(
							"rounded-lg transition-all duration-200",
							"hover:bg-slate-100 hover:text-sky-600",
							"active:scale-95 active:bg-slate-200",
							"text-slate-500",
							containersLoading || isManualRefetching
								? "cursor-wait opacity-70"
								: "",
						)}
						title="목록 새로고침"
					>
						<RefreshCw
							size={18}
							className={clsx(
								"transition-all",
								(containersLoading ||
									containersRefetching ||
									isManualRefetching) &&
									"animate-spin text-sky-600",
							)}
						/>
					</Button>
				</Card.Header>

				<Card.Body className="pt-6">
					{!dockerHealth?.available ? (
						<div className="text-center py-10 text-slate-500 bg-slate-50 rounded-xl border border-slate-100 border-dashed">
							<AlertCircle size={32} className="mx-auto mb-3 text-slate-400" />
							<p className="font-medium text-slate-600">
								Docker 서비스에 연결할 수 없습니다
							</p>
							<p className="text-xs text-slate-400 mt-1">
								Docker 소켓이 마운트되어 있는지 확인하세요.
							</p>
						</div>
					) : containersLoading ? (
						<div className="text-center py-12 text-slate-400">
							<RefreshCw
								size={24}
								className="animate-spin mx-auto mb-2 opacity-50"
							/>
							컨테이너 상태를 불러오는 중…
						</div>
					) : containers.length === 0 ? (
						<div className="text-center py-10 text-slate-400 bg-slate-50 rounded-xl border border-slate-100 border-dashed">
							관리 대상 컨테이너가 없습니다.
						</div>
					) : (
						<VirtualList
							items={containers}
							estimateSize={() => 108}
							getItemKey={(container) => container.id}
							recomputeKey={actionInProgress}
							className="max-h-[34rem] pr-1"
							itemClassName="pb-3"
							renderItem={(container: DockerContainer) => (
								<DockerContainerItem
									key={container.id}
									container={container}
									actionInProgress={actionInProgress}
									onAction={openConfirmModal}
								/>
							)}
						/>
					)}
				</Card.Body>
			</Card>

			<DockerContainerConfirmModal
				confirmModal={confirmModal}
				onClose={closeConfirmModal}
				onConfirm={handleConfirmAction}
			/>
		</>
	);
};
