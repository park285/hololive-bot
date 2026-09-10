import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { queryKeys } from "@/queries/keys";
import { RequestBlockedError } from "@/api/errors";
import { operations } from "@/app/bootstrap";
import { dockerApi } from "@/features/docker/api";
import toast from "@/lib/toast-api";
import { useBusinessMutation } from "@/operations/useBusinessMutation";
import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";

export type ContainerAction = "restart" | "stop" | "start";
export interface ContainerIntent { name: string; action: ContainerAction }
export const actionLabels: Record<ContainerAction, string> = { restart: "재시작", stop: "중지", start: "시작" };

/** useContainers는 확인한 정책·조회 상태와 앱 업무 경계 안에서 Docker 변경을 한 번 실행합니다. */
export function useContainers() {
	const queryClient = useQueryClient();
	const online = useOnline();
	const [confirmation, setConfirmation] = useState<ContainerIntent | null>(null);
	const healthQuery = useQuery({ queryKey: queryKeys.docker.health, queryFn: dockerApi.checkHealth, refetchInterval: 30000, retry: 1 });
	const containersQuery = useQuery({ queryKey: queryKeys.docker.containers, queryFn: dockerApi.getContainers, enabled: healthQuery.data?.available === true, refetchInterval: 15000 });
	const healthView = queryView(healthQuery, online, () => false);
	const containersView = queryView(containersQuery, online, data => data.containers.length === 0);
	const invalidate = () => { void queryClient.invalidateQueries({ queryKey: queryKeys.docker.containers }); };
	const mutation = useBusinessMutation({
		mutationFn: (intent: ContainerIntent) => ({ restart: dockerApi.restartContainer, stop: dockerApi.stopContainer, start: dockerApi.startContainer })[intent.action](intent.name),
		onSuccess: (_data, intent) => { toast.success(`${intent.name} ${actionLabels[intent.action]} 작업을 완료했습니다.`); invalidate(); },
		onError: error => {
			toast.error(operations.failure(error)?.message ?? (error instanceof RequestBlockedError ? error.message : "컨테이너 작업 결과를 확인하지 못했습니다. 현재 상태를 다시 조회해 주세요."));
			invalidate();
		},
	});
	const current = healthView.current && healthView.data.available && containersView.current;
	const allowed = (intent: ContainerIntent) => {
		const target = containersView.data?.containers.find(container => container.name === intent.name);
		return current && target?.managed === true && (intent.action !== "stop" || !target.stopBlocked);
	};
	const open = (name: string, action: ContainerAction) => { setConfirmation({ name, action }); };
	const close = () => { setConfirmation(null); };
	const confirm = () => {
		if (confirmation === null || !allowed(confirmation)) return;
		mutation.mutate(confirmation);
		close();
	};
	const refresh = () => { void healthQuery.refetch(); void containersQuery.refetch(); };
	return {
		healthQuery, containersQuery, healthView, containersView, current,
		confirmation, canConfirm: confirmation !== null && allowed(confirmation), open, close, confirm, refresh,
		actionInProgress: mutation.isPending ? mutation.variables?.name ?? null : null,
	};
}
