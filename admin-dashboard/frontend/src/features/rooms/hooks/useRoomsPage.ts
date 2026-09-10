import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";
import { useBusinessMutation } from "@/operations/useBusinessMutation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { queryKeys } from "@/queries/keys";
import { roomsApi } from "@/features/rooms/api";
import type { ACLMode } from "@/features/rooms/types";

export const MODE_LABELS: Record<
	ACLMode,
	{
		listTitle: string;
		emptyText: string;
		removeConfirm: string;
		description: string;
		indicator: string;
	}
> = {
	whitelist: {
		listTitle: "채팅방 접근 목록",
		emptyText: "관리할 채팅방이 없습니다.",
		removeConfirm:
			"이 채팅방의 허용을 해제하시겠습니까? 해제 후에는 봇이 이 방의 명령에 응답하지 않습니다.",
		description:
			"화이트리스트 모드입니다. 등록된 채팅방에서만 봇이 작동합니다.",
		indicator: "bg-emerald-400",
	},
	blacklist: {
		listTitle: "채팅방 접근 목록",
		emptyText: "관리할 채팅방이 없습니다.",
		removeConfirm:
			"이 채팅방의 차단을 해제하시겠습니까? 해제 후에는 봇이 이 방의 명령에 다시 응답합니다.",
		description:
			"블랙리스트 모드입니다. 등록된 채팅방에서는 봇이 작동하지 않습니다.",
		indicator: "bg-rose-400",
	},
};

export function useRoomsPage() {
	const queryClient = useQueryClient();
	const [newRoom, setNewRoom] = useState("");
	const [removeModal, setRemoveModal] = useState<{
		isOpen: boolean;
		room: string;
	}>({ isOpen: false, room: "" });

	const query = useQuery({
		queryKey: queryKeys.rooms.all,
		queryFn: roomsApi.getAll,
	});

	const joinedQuery = useQuery({
		queryKey: queryKeys.rooms.joined,
		queryFn: roomsApi.getJoined,
		retry: false,
		staleTime: 1000 * 60,
	});

	const online = useOnline();
	const view = queryView(query, online, data => data.rooms.length === 0);
	const joinedView = queryView(joinedQuery, online, data => data.rooms.length === 0);
	const invalidate = () => { void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all }); };

	const addRoomMutation = useBusinessMutation({
		mutationFn: roomsApi.add,
		onSuccess: (_data, request) => {
			setNewRoom(current => current.trim() === request.room ? "" : current);
			 invalidate();
		},
		onError: invalidate,
	});

	const removeRoomMutation = useBusinessMutation({
		mutationFn: roomsApi.remove,
		onSuccess: invalidate,
		onError: invalidate,
	});

	const setACLMutation = useBusinessMutation({
		mutationFn: roomsApi.setACL,
		onSuccess: invalidate,
		onError: invalidate,
	});

	const handleAddRoom = () => {
		const room = newRoom.trim();
		if (!room || !view.current) return;
		removeRoomMutation.reset();
		addRoomMutation.mutate({ room });
	};

	const handleAddRoomId = (chatId: string) => {
		const room = chatId.trim();
		if (!room || !view.current) return;
		removeRoomMutation.reset();
		addRoomMutation.mutate({ room });
	};

	const confirmRemoveRoom = () => {
		if (!removeModal.room || removeRoomMutation.isPending || !view.current) return;
		addRoomMutation.reset();
		void removeRoomMutation.mutateAsync({ room: removeModal.room }).then(() => {
			setRemoveModal(current => current === removeModal ? { isOpen: false, room: "" } : current);
		}, () => { /* OperationNotice와 확인창이 결과를 표시하며 초안을 보존합니다. */ });
	};

	const handleToggleACL = () => {
		if (!view.current) return;
		setACLMutation.mutate({ enabled: !view.data.aclEnabled });
	};

	const handleModeChange = (mode: ACLMode) => {
		if (!view.current || mode === view.data.aclMode) return;
		setACLMutation.mutate({ mode });
	};

	return {
		newRoom,
		setNewRoom,
		removeModal,
		setRemoveModal,
		query,
		view,
		joinedQuery,
		joinedView,
		addRoomMutation,
		removeRoomMutation,
		setACLMutation,
		handleAddRoom,
		handleAddRoomId,
		confirmRemoveRoom,
		handleToggleACL,
		handleModeChange,
	};
}
