import { QueryNotice } from "@/queries/QueryNotice";
import { RequestBlockedError } from "@/api/errors";
import { ConfirmModal } from "@/components/ConfirmModal";
import EditNameModal from "@/components/EditNameModal";
import { AlarmGroups } from "@/features/alarms/components/AlarmGroups";
import { AlarmsToolbar } from "@/features/alarms/components/AlarmsToolbar";
import { useAlarmsPage } from "@/features/alarms/hooks/useAlarmsPage";

export const AlarmsPage = () => {
	const {
		search,
		setSearch,
		expandedGroups,
		setExpandedGroups,
		alarmToDelete,
		setAlarmToDelete,
		visibleGroupCount,
		setVisibleGroupCount,
		editModal,
		setEditModal,
		filteredGroups,
		totalAlarms,
		query,
		view,
		deleteAlarmMutation,
		setNameMutation,
	} = useAlarmsPage();

	const toggleGroup = (groupKey: string) => {
		const nextExpandedGroups = new Set(expandedGroups);
		if (nextExpandedGroups.has(groupKey)) {
			nextExpandedGroups.delete(groupKey);
		} else {
			nextExpandedGroups.add(groupKey);
		}
		setExpandedGroups(nextExpandedGroups);
	};

	const confirmDelete = () => {
		if (!alarmToDelete || !view.current) return;

		deleteAlarmMutation.mutate({
			roomId: alarmToDelete.roomId,
			channelId: alarmToDelete.channelId,
		});
		setAlarmToDelete(null);
	};

	const handleSaveName = async (newName: string) => {
		if (!view.current) throw new RequestBlockedError("CLIENT_NOT_READY", "최신 알람 목록을 먼저 조회해 주세요.");
		if (!editModal) return;

		await setNameMutation.mutateAsync({
			type: editModal.type,
			id: editModal.id,
			name: newName,
		});
		setEditModal(current => current === editModal ? null : current);
	};

	const readState = <QueryNotice view={view} label="알람 목록" onRetry={() => { void query.refetch(); }} />;
	if (view.data === undefined) return readState;
	const latestRoom = editModal ? query.data?.alarms.find(alarm => alarm.roomId === editModal.id) : undefined;

	return (
		<div className="space-y-6">
			{readState}
			{view.kind === "empty" && <p role="status">등록된 알람이 없습니다.</p>}
			<AlarmsToolbar
				search={search}
				onSearchChange={setSearch}
				groupCount={filteredGroups.length}
				alarmCount={totalAlarms}
			/>

			<fieldset disabled={!view.current} className="min-w-0">
			<AlarmGroups
				groups={filteredGroups}
				expandedGroups={expandedGroups}
				onToggleGroup={toggleGroup}
				onDeleteAlarm={setAlarmToDelete}
				onEditName={(type, id, currentName) => {
					setEditModal({ type, id, currentName });
				}}
				visibleGroupCount={visibleGroupCount}
				onLoadMore={() => {
					setVisibleGroupCount((prev) => prev + 20);
				}}
				isDeleting={deleteAlarmMutation.isPending}
			/>

			</fieldset>

			{editModal && <EditNameModal
				isOpen
				key={editModal.id}
				canSubmit={view.current && latestRoom !== undefined}
				pending={setNameMutation.isPending}
				readState={readState}
				onClose={() => {
					setEditModal(null);
				}}
				type={editModal.type}
				id={editModal.id}
				currentName={latestRoom?.roomName}
				onSave={handleSaveName}
			/>}

			<ConfirmModal
				isOpen={alarmToDelete !== null}
				onClose={() => {
					setAlarmToDelete(null);
				}}
				onConfirm={confirmDelete}
				canConfirm={view.current}
				isPending={deleteAlarmMutation.isPending}
				title="알람 삭제"
				message={
					alarmToDelete ? "이 방에서 해당 채널의 알람 구독을 모두 삭제하시겠습니까?" : ""
				}
				confirmText="삭제"
				confirmColor="danger"
			>
				{alarmToDelete && (
					<div className="bg-muted p-4 rounded-lg mt-2 border border-border-subtle flex flex-col gap-2">
						<div className="flex justify-between items-center text-sm">
							<span className="text-muted-foreground">멤버</span>
							<span className="font-bold text-foreground">
								{alarmToDelete.memberName || "이름 없음"}
							</span>
						</div>
						<div className="flex justify-between items-center text-sm">
							<span className="text-muted-foreground">채널 ID</span>
							<span className="font-mono text-muted-foreground text-xs">
								{alarmToDelete.channelId}
							</span>
						</div>
					</div>
				)}
			</ConfirmModal>
		</div>
	);
};
