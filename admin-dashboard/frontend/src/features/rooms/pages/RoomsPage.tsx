import { ConfirmModal } from "@/components/ConfirmModal";
import { Button } from "@/components/ui/Button";
import { RoomsAclSection } from "@/features/rooms/components/RoomsAclSection";
import { RoomsListSection } from "@/features/rooms/components/RoomsListSection";
import { useRoomsPage } from "@/features/rooms/hooks/useRoomsPage";

export const RoomsPage = () => {
	const {
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
	} = useRoomsPage();

	if (query.isLoading) {
		return (
			<div
				className="text-center py-24 text-muted-foreground"
				aria-busy="true"
				aria-label="데이터를 불러오는 중입니다…"
			>
				<div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
				<p>데이터를 불러오는 중입니다…</p>
			</div>
		);
	}

	if (query.isError) {
		return (
			<div
				role="alert"
				className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100"
			>
				<div className="text-rose-600 font-bold mb-2">
					채팅방 목록을 불러올 수 없습니다
				</div>
				<div className="text-xs text-rose-500 mb-4">
					{query.error instanceof Error
						? query.error.message
						: "알 수 없는 오류가 발생했습니다"}
				</div>
				<Button
					onClick={() => {
						void query.refetch();
					}}
					className="bg-rose-600 hover:bg-rose-700 text-white focus-visible:ring-2 focus-visible:ring-rose-200"
					aria-label="데이터 다시 불러오기"
				>
					다시 시도
				</Button>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<RoomsAclSection
				aclEnabled={aclEnabled}
				aclMode={aclMode}
				description={
					aclEnabled
						? labels.description
						: "접근 제어가 비활성화되었습니다. 모든 채팅방에서 봇이 명령을 수행합니다."
				}
				isPending={setACLMutation.isPending}
				onToggleACL={handleToggleACL}
				onModeChange={handleModeChange}
			/>

			<RoomsListSection
				rooms={rooms}
				listTitle={labels.listTitle}
				emptyText={labels.emptyText}
				addTitle={labels.addTitle}
				indicatorClassName={labels.indicator}
				isBlacklist={isBlacklist}
				infoMessage={
					isBlacklist
						? "차단 목록에 추가된 채팅방에서는 봇이 명령에 응답하지 않습니다."
						: "오픈프로필 채팅방의 경우, 봇이 방에 입장해 있어야 ID를 확인할 수 있습니다."
				}
				newRoom={newRoom}
				onNewRoomChange={setNewRoom}
				onAddRoom={handleAddRoom}
				onDeleteRoom={(room) => {
					setDeleteModal({ isOpen: true, room });
				}}
				addPending={addRoomMutation.isPending}
				removePending={removeRoomMutation.isPending}
			/>

			<ConfirmModal
				isOpen={deleteModal.isOpen}
				onClose={() => {
					setDeleteModal({ isOpen: false, room: "" });
				}}
				onConfirm={confirmDelete}
				title={isBlacklist ? "차단 해제" : "채팅방 삭제"}
				message={labels.deleteConfirm}
				confirmText="삭제"
				confirmColor="danger"
			>
				{deleteModal.room && (
					<div className="bg-muted p-3 rounded-lg mt-2 text-center font-mono font-bold text-foreground border border-border">
						{deleteModal.room}
					</div>
				)}
			</ConfirmModal>
		</div>
	);
};
