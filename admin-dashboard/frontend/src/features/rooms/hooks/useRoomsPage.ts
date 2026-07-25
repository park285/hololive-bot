import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { queryKeys } from "@/api/queryKeys";
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

	const addRoomMutation = useMutation({
		mutationFn: roomsApi.add,
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
			setNewRoom("");
		},
	});

	const removeRoomMutation = useMutation({
		mutationFn: roomsApi.remove,
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
			setRemoveModal({ isOpen: false, room: "" });
		},
	});

	const setACLMutation = useMutation({
		mutationFn: roomsApi.setACL,
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
		},
	});

	const rooms = query.data?.rooms ?? [];
	const aclEnabled = query.data?.aclEnabled ?? true;
	const aclMode = (query.data?.aclMode ?? "blacklist") as ACLMode;
	const labels = MODE_LABELS[aclMode];
	const isBlacklist = aclMode === "blacklist";

	const joinedRooms = joinedQuery.data?.rooms ?? [];

	const handleAddRoom = () => {
		const room = newRoom.trim();
		if (!room) return;
		removeRoomMutation.reset();
		addRoomMutation.mutate({ room });
	};

	const handleAddRoomId = (chatId: string) => {
		const room = chatId.trim();
		if (!room) return;
		removeRoomMutation.reset();
		addRoomMutation.mutate({ room });
	};

	const confirmRemoveRoom = () => {
		if (!removeModal.room || removeRoomMutation.isPending) return;
		addRoomMutation.reset();
		removeRoomMutation.mutate({ room: removeModal.room });
	};

	const handleToggleACL = () => {
		setACLMutation.mutate({ enabled: !aclEnabled });
	};

	const handleModeChange = (mode: ACLMode) => {
		if (mode === aclMode) return;
		setACLMutation.mutate({ mode });
	};

	return {
		newRoom,
		setNewRoom,
		removeModal,
		setRemoveModal,
		query,
		addRoomMutation,
		removeRoomMutation,
		setACLMutation,
		rooms,
		aclEnabled,
		aclMode,
		labels,
		isBlacklist,
		joinedRooms,
		joinedLoading: joinedQuery.isLoading,
		joinedUnavailable: joinedQuery.isError,
		handleAddRoom,
		handleAddRoomId,
		confirmRemoveRoom,
		handleToggleACL,
		handleModeChange,
	};
}
