import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import AlertCircle from "lucide-react/dist/esm/icons/alert-circle";
import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle";
import RefreshCw from "lucide-react/dist/esm/icons/refresh-cw";
import Server from "lucide-react/dist/esm/icons/server";
import { useState } from "react";
import {
	type DockerContainer,
	dockerApi,
	type StatusOnlyResponse,
} from "@/api/core";
import { queryKeys } from "@/api/queryKeys";
import { ConfirmModal } from "@/components/ConfirmModal";
import { DockerContainerItem } from "@/components/docker/DockerContainerItem";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { VirtualList } from "@/components/ui/VirtualList";
import toast from "@/lib/toast-api";

interface DockerContainerListProps {
	initialHealth?: { status: string; available: boolean };
	initialContainers?: { status: string; containers: DockerContainer[] };
}

export const DockerContainerList = ({
	initialHealth,
	initialContainers,
}: DockerContainerListProps) => {
	const queryClient = useQueryClient();
	const [isManualRefetching, setIsManualRefetching] = useState(false);
	const [actionInProgress, setActionInProgress] = useState<string | null>(null);

	const [confirmModal, setConfirmModal] = useState<{
		isOpen: boolean;
		containerName: string | null;
		action: "restart" | "stop" | "start" | null;
	}>({
		isOpen: false,
		containerName: null,
		action: null,
	});

	const { data: dockerHealth } = useQuery({
		queryKey: queryKeys.docker.health,
		queryFn: dockerApi.checkHealth,
		refetchInterval: 30000,
		retry: 1,
		initialData: initialHealth,
	});

	const safeInitialContainers =
		initialContainers && initialContainers.status
			? initialContainers
			: undefined;

	const {
		data: containersData,
		isLoading: containersLoading,
		isRefetching: containersRefetching,
		refetch: refetchContainers,
	} = useQuery({
		queryKey: queryKeys.docker.containers,
		queryFn: dockerApi.getContainers,
		enabled: dockerHealth?.available === true,
		refetchInterval: 15000,
		initialData: safeInitialContainers,
	});

	const restartMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.restartContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			setActionInProgress(null);
			toast.success(
				<span>
					<span className="font-bold text-slate-800">{containerName}</span>
					<span className="text-slate-600"> 재시작을 요청했습니다.</span>
				</span>,
			);
			void queryClient.invalidateQueries({
				queryKey: queryKeys.docker.containers,
			});
		},
		onError: () => {
			setActionInProgress(null);
		},
	});

	const stopMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.stopContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			setActionInProgress(null);
			toast.success(
				<span>
					<span className="font-bold text-slate-800">{containerName}</span>
					<span className="text-slate-600"> 중지되었습니다.</span>
				</span>,
			);
			void queryClient.invalidateQueries({
				queryKey: queryKeys.docker.containers,
			});
		},
		onError: () => {
			setActionInProgress(null);
		},
	});

	const startMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.startContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			setActionInProgress(null);
			toast.success(
				<span>
					<span className="font-bold text-slate-800">{containerName}</span>
					<span className="text-slate-600"> 시작되었습니다.</span>
				</span>,
			);
			void queryClient.invalidateQueries({
				queryKey: queryKeys.docker.containers,
			});
		},
		onError: () => {
			setActionInProgress(null);
		},
	});

	const openConfirmModal = (
		containerName: string,
		action: "restart" | "stop" | "start",
	) => {
		setConfirmModal({ isOpen: true, containerName, action });
	};

	const closeConfirmModal = () => {
		setConfirmModal({ isOpen: false, containerName: null, action: null });
	};

	const handleConfirmAction = () => {
		if (confirmModal.containerName && confirmModal.action) {
			const name = confirmModal.containerName;
			setActionInProgress(name);

			switch (confirmModal.action) {
				case "restart":
					restartMutation.mutate(name);
					break;
				case "stop":
					stopMutation.mutate(name);
					break;
				case "start":
					startMutation.mutate(name);
					break;
			}
			closeConfirmModal();
		}
	};

	const handleRefresh = async () => {
		setIsManualRefetching(true);
		const minDelay = new Promise((resolve) => setTimeout(resolve, 500));
		try {
			await Promise.all([refetchContainers(), minDelay]);
			toast.success("컨테이너 상태를 갱신했습니다", {
				id: "refresh-containers",
			});
		} catch {
			toast.error("갱신 실패");
		} finally {
			setIsManualRefetching(false);
		}
	};

	const getActionLabel = (action: "restart" | "stop" | "start" | null) => {
		switch (action) {
			case "restart":
				return "재시작";
			case "stop":
				return "중지";
			case "start":
				return "시작";
			default:
				return "";
		}
	};

	const containers = containersData?.containers ?? [];

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
						disabled={containersLoading || isManualRefetching}
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

			<ConfirmModal
				isOpen={confirmModal.isOpen}
				onClose={closeConfirmModal}
				onConfirm={handleConfirmAction}
				title={`컨테이너 ${getActionLabel(confirmModal.action)}`}
				message={""}
				confirmText={`${getActionLabel(confirmModal.action)} 실행`}
				confirmColor={
					confirmModal.action === "stop"
						? "danger"
						: confirmModal.action === "start"
							? "primary"
							: "primary"
				}
			>
				<div className="space-y-3">
					<div className="flex items-center gap-3 mb-2">
						<div className="bg-amber-100 p-2.5 rounded-full shrink-0">
							<AlertTriangle className="text-amber-600" size={24} />
						</div>
						<p className="text-slate-600">
							'
							<span className="font-bold text-slate-800">
								{confirmModal.containerName}
							</span>
							'을(를) {getActionLabel(confirmModal.action)}하시겠습니까?
						</p>
					</div>

					{confirmModal.action !== "start" && (
						<p className="text-xs text-amber-600 font-medium bg-amber-50 px-3 py-2 rounded-lg">
							주의: 서비스가 잠시 중단될 수 있습니다.
						</p>
					)}
				</div>
			</ConfirmModal>
		</>
	);
};
