import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { queryKeys } from "@/api/queryKeys";
import { roomsApi } from "@/features/rooms/api";
import type { ACLMode } from "@/features/rooms/types";

export const MODE_LABELS: Record<
	ACLMode,
	{
		title: string;
		listTitle: string;
		emptyText: string;
		addTitle: string;
		deleteConfirm: string;
		description: string;
		indicator: string;
	}
> = {
	whitelist: {
		title: "화이트리스트",
		listTitle: "허용된 채팅방 목록",
		emptyText: "허용된 방이 없습니다.",
		addTitle: "허용 방 추가",
		deleteConfirm: "정말 이 채팅방을 허용 목록에서 삭제하시겠습니까?",
		description:
			"화이트리스트 모드입니다. 등록된 채팅방에서만 봇이 작동합니다.",
		indicator: "bg-emerald-400",
	},
	blacklist: {
		title: "블랙리스트",
		listTitle: "차단된 채팅방 목록",
		emptyText: "차단된 방이 없습니다.",
		addTitle: "차단 방 추가",
		deleteConfirm: "정말 이 채팅방을 차단 목록에서 삭제하시겠습니까?",
		description:
			"블랙리스트 모드입니다. 등록된 채팅방에서는 봇이 작동하지 않습니다.",
		indicator: "bg-rose-400",
	},
};

export function useRoomsPage() {
	const queryClient = useQueryClient();
	const [newRoom, setNewRoom] = useState("");
	const [deleteModal, setDeleteModal] = useState<{
		isOpen: boolean;
		room: string;
	}>({ isOpen: false, room: "" });

	const query = useQuery({
		queryKey: queryKeys.rooms.all,
		queryFn: roomsApi.getAll,
	});

	const addRoomMutation = useMutation({
		mutationFn: roomsApi.add,
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
			setNewRoom("");
		},
	});

	const removeRoomMutation = useMutation({
		mutationFn: roomsApi.remove,
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
		},
	});

	const setACLMutation = useMutation({
		mutationFn: roomsApi.setACL,
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all });
		},
	});

	const rooms = query.data?.rooms ?? [];
	const aclEnabled = query.data?.aclEnabled ?? true;
	const aclMode = (query.data?.aclMode ?? "blacklist") as ACLMode;
	const labels = MODE_LABELS[aclMode];
	const isBlacklist = aclMode === "blacklist";

	const handleAddRoom = () => {
		const room = newRoom.trim();
		if (!room) return;
		void addRoomMutation.mutateAsync({ room });
	};

	const confirmDelete = () => {
		if (deleteModal.room) {
			void removeRoomMutation.mutateAsync({ room: deleteModal.room });
		}
		setDeleteModal({ isOpen: false, room: "" });
	};

	const handleToggleACL = () => {
		void setACLMutation.mutateAsync({ enabled: !aclEnabled });
	};

	const handleModeChange = (mode: ACLMode) => {
		if (mode === aclMode) return;
		void setACLMutation.mutateAsync({ mode });
	};

	return {
		newRoom,
		setNewRoom,
		deleteModal,
		setDeleteModal,
		query,
		addRoomMutation,
		removeRoomMutation,
		setACLMutation,
		rooms,
		aclEnabled,
		aclMode,
		labels,
		isBlacklist,
		handleAddRoom,
		confirmDelete,
		handleToggleACL,
		handleModeChange,
	};
}
