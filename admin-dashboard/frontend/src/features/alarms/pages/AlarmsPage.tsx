import Bell from "lucide-react/dist/esm/icons/bell";
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
		groupedAlarms,
		filteredGroups,
		totalAlarms,
		query,
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
		if (!alarmToDelete) return;

		void deleteAlarmMutation.mutateAsync({
			roomId: alarmToDelete.roomId,
			userId: alarmToDelete.userId,
			channelId: alarmToDelete.channelId,
		});
		setAlarmToDelete(null);
	};

	const handleSaveName = (newName: string) => {
		if (!editModal) return;

		void setNameMutation.mutateAsync({
			type: editModal.type,
			id: editModal.id,
			name: newName,
		});
	};

	if (query.isLoading) {
		return (
			<div
				className="text-center py-24 text-slate-500"
				aria-busy="true"
				aria-label="알람 데이터를 불러오는 중입니다…"
			>
				<div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
				<p>로딩 중…</p>
			</div>
		);
	}

	if (groupedAlarms.length === 0) {
		return (
			<div className="text-center py-12 bg-white rounded-2xl border border-slate-100 shadow-sm">
				<Bell
					className="mx-auto h-12 w-12 text-slate-200 mb-4"
					aria-hidden="true"
				/>
				<p className="text-slate-500 font-medium">등록된 알람이 없습니다</p>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<AlarmsToolbar
				search={search}
				onSearchChange={setSearch}
				groupCount={filteredGroups.length}
				alarmCount={totalAlarms}
			/>

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

			<EditNameModal
				isOpen={editModal !== null}
				onClose={() => {
					setEditModal(null);
				}}
				type={editModal?.type || "room"}
				id={editModal?.id || ""}
				currentName={editModal?.currentName || ""}
				onSave={handleSaveName}
			/>

			<ConfirmModal
				isOpen={alarmToDelete !== null}
				onClose={() => {
					setAlarmToDelete(null);
				}}
				onConfirm={confirmDelete}
				title="알람 삭제"
				message={
					alarmToDelete ? "다음 멤버의 알람 설정을 삭제하시겠습니까?" : ""
				}
				confirmText="삭제"
				confirmColor="danger"
			>
				{alarmToDelete && (
					<div className="bg-slate-50 p-4 rounded-lg mt-2 border border-slate-100 flex flex-col gap-2">
						<div className="flex justify-between items-center text-sm">
							<span className="text-slate-500">멤버</span>
							<span className="font-bold text-slate-800">
								{alarmToDelete.memberName || "이름 없음"}
							</span>
						</div>
						<div className="flex justify-between items-center text-sm">
							<span className="text-slate-500">채널 ID</span>
							<span className="font-mono text-slate-600 text-xs">
								{alarmToDelete.channelId}
							</span>
						</div>
					</div>
				)}
			</ConfirmModal>
		</div>
	);
};
