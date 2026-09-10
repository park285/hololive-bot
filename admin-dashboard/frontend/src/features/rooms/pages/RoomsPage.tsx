import { ConfirmModal } from "@/components/ConfirmModal";
import { QueryNotice } from "@/queries/QueryNotice";
import { operations } from "@/app/bootstrap";
import { RoomsAclSection } from "@/features/rooms/components/RoomsAclSection";
import { RoomsListSection } from "@/features/rooms/components/RoomsListSection";
import { MODE_LABELS, useRoomsPage } from "@/features/rooms/hooks/useRoomsPage";

export const RoomsPage = () => {
	const {
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
	} = useRoomsPage();

	const readState = <QueryNotice view={view} label="채팅방 접근 설정" onRetry={() => { void query.refetch(); }} />;
	if (view.data === undefined) return readState;
	const { rooms, aclEnabled, aclMode } = view.data;
	const labels = MODE_LABELS[aclMode];
	const isBlacklist = aclMode === "blacklist";

	return (
		<div className="space-y-6">
			{readState}
			<QueryNotice view={joinedView} label="참여 중인 채팅방" onRetry={() => { void joinedQuery.refetch(); }} />
			<fieldset disabled={!view.current} className="space-y-6 min-w-0">
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
				onAddRoomId={handleAddRoomId}
				onRemoveRoom={(room) => {
					removeRoomMutation.reset();
					setRemoveModal({ isOpen: true, room });
				}}
				addPending={addRoomMutation.isPending}
				removePending={removeRoomMutation.isPending}
				joinedRooms={joinedView.data?.rooms ?? []}
				joinedLoading={joinedView.kind === "pending"}
				joinedUnavailable={!joinedView.current}
				actionError={addRoomMutation.error ?? removeRoomMutation.error}
			/>

			</fieldset>

			<ConfirmModal
				isOpen={removeModal.isOpen}
				onClose={() => {
					if (!removeRoomMutation.isPending) {
						setRemoveModal({ isOpen: false, room: "" });
					}
				}}
				onConfirm={confirmRemoveRoom}
				canConfirm={view.current}
				title={isBlacklist ? "차단 해제" : "허용 해제"}
				message={labels.removeConfirm}
				confirmText={
					removeRoomMutation.isPending
						? "처리 중…"
						: isBlacklist
							? "차단 해제"
							: "허용 해제"
				}
				confirmColor={isBlacklist ? "primary" : "danger"}
				isPending={removeRoomMutation.isPending}
			>
				{removeModal.room && (
					<div className="bg-muted p-3 rounded-lg mt-2 text-center font-mono font-bold text-foreground border border-border">
						{removeModal.room}
					</div>
				)}
				{removeRoomMutation.isError && (
					<div
						role="alert"
						className="mt-3 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700"
					>
						{operations.failure(removeRoomMutation.error)?.message ?? "요청 결과를 확인하지 못했습니다. 현재 상태를 다시 조회해 주세요."}
					</div>
				)}
			</ConfirmModal>
		</div>
	);
};
