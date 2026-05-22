import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
	type DockerContainer,
	dockerApi,
	type StatusOnlyResponse,
} from "@/api/core";
import { queryKeys } from "@/api/queryKeys";
import toast from "@/lib/toast-api";
import { getErrorMessageFromUnknown } from "@/lib/typeUtils";

export type ContainerAction = "restart" | "stop" | "start";

export interface ConfirmModalState {
	isOpen: boolean;
	containerName: string | null;
	action: ContainerAction | null;
}

interface UseDockerContainerActionsProps {
	initialHealth?: { status: string; available: boolean };
	initialContainers?: { status: string; containers: DockerContainer[] };
}

export function useDockerContainerActions({
	initialHealth,
	initialContainers,
}: UseDockerContainerActionsProps) {
	const queryClient = useQueryClient();
	const [isManualRefetching, setIsManualRefetching] = useState(false);
	const [actionInProgress, setActionInProgress] = useState<string | null>(null);

	const [confirmModal, setConfirmModal] = useState<ConfirmModalState>({
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

	const onMutationSuccess = (
		containerName: string,
		message: string,
	) => {
		setActionInProgress(null);
		toast.success(
			<span>
				<span className="font-bold text-slate-800">{containerName}</span>
				<span className="text-slate-600"> {message}</span>
			</span>,
		);
		void queryClient.invalidateQueries({
			queryKey: queryKeys.docker.containers,
		});
	};

	const onMutationError = (error: unknown) => {
		setActionInProgress(null);
		toast.error(`컨테이너 작업 실패: ${getErrorMessageFromUnknown(error)}`);
	};

	const restartMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.restartContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			onMutationSuccess(containerName, "재시작을 요청했습니다.");
		},
		onError: onMutationError,
	});

	const stopMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.stopContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			onMutationSuccess(containerName, "중지되었습니다.");
		},
		onError: onMutationError,
	});

	const startMutation = useMutation({
		mutationFn: (containerName: string) =>
			dockerApi.startContainer(containerName),
		onSuccess: (_data: StatusOnlyResponse, containerName: string) => {
			onMutationSuccess(containerName, "시작되었습니다.");
		},
		onError: onMutationError,
	});

	const openConfirmModal = (
		containerName: string,
		action: ContainerAction,
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
			const [result] = await Promise.all([refetchContainers(), minDelay]);
			if (result.error) {
				throw result.error;
			}
			toast.success("컨테이너 상태를 갱신했습니다", {
				id: "refresh-containers",
			});
		} catch (error) {
			toast.error(`갱신 실패: ${getErrorMessageFromUnknown(error)}`);
		} finally {
			setIsManualRefetching(false);
		}
	};

	const containers = containersData?.containers ?? [];

	return {
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
	};
}

export function getActionLabel(action: ContainerAction | null): string {
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
}
