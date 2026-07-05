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
import { QuerySection } from "@/components/ui/QuerySection";
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
		containersError,
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
			<Card className="relative overflow-hidden">
				<div className="absolute top-0 left-0 right-0 h-1 bg-linear-to-r from-indigo-400 to-sky-400" />
				<Card.Header className="flex flex-row flex-wrap items-center justify-between gap-2 border-b border-border-subtle pb-4">
					<div className="flex items-center gap-2">
						<span className="flex items-center justify-center w-9 h-9 rounded-xl bg-linear-to-br from-indigo-400 to-indigo-500 text-white shadow-sm shadow-indigo-200/50"><Server size={18} /></span>
						<h3 className="text-lg font-bold text-foreground">컨테이너 관리</h3>
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
							"hover:bg-accent hover:text-sky-600",
							"active:scale-95 active:bg-border",
							"text-muted-foreground",
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
						<div className="text-center py-10 text-muted-foreground bg-muted rounded-xl border border-border-subtle border-dashed">
							<AlertCircle size={32} className="mx-auto mb-3 text-subtle-foreground" />
							<p className="font-medium text-muted-foreground">
								Docker 서비스에 연결할 수 없습니다
							</p>
							<p className="text-xs text-subtle-foreground mt-1">
								Docker 소켓이 마운트되어 있는지 확인하세요.
							</p>
						</div>
					) : (
						<QuerySection
							isLoading={containersLoading}
							isError={containersError && containers.length === 0}
							onRetry={() => {
								void handleRefresh();
							}}
							isEmpty={containers.length === 0}
							skeleton={
								<div className="text-center py-12 text-subtle-foreground">
									<RefreshCw
										size={24}
										className="animate-spin mx-auto mb-2 opacity-50"
									/>
									컨테이너 상태를 불러오는 중…
								</div>
							}
							emptyContent={
								<div className="text-center py-10 text-subtle-foreground bg-muted rounded-xl border border-border-subtle border-dashed">
									관리 대상 컨테이너가 없습니다.
								</div>
							}
						>
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
						</QuerySection>
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
