import AddMemberModal from "@/components/AddMemberModal";
import ChannelEditModal from "@/components/ChannelEditModal";
import { ConfirmModal } from "@/components/ConfirmModal";
import EditNameModal from "@/components/EditNameModal";
import { MembersGrid } from "@/features/members/components/MembersGrid";
import { MembersToolbar } from "@/features/members/components/MembersToolbar";
import { useMembersPage } from "@/features/members/hooks/useMembersPage";
import type { Member } from "@/features/members/types";

export const MembersPage = () => {
	const {
		query,
		mutations,
		searchTerm,
		setSearchTerm,
		hideGraduated,
		toggleHideGraduated,
		modal,
		setModal,
		isAddModalOpen,
		setIsAddModalOpen,
		visibleCount,
		setVisibleCount,
		allMembers,
		sortedMembers,
		visibleMembers,
		handleAddAlias,
		handleRemoveAlias,
		confirmRemoveAlias,
		handleUpdateChannel,
		confirmUpdateChannel,
		handleEditName,
		confirmEditName,
		handleToggleGraduation,
		confirmToggleGraduation,
	} = useMembersPage();

	if (query.isLoading) {
		return (
			<div
				className="text-center py-24 text-slate-500"
				aria-busy="true"
				aria-label="데이터를 불러오는 중입니다…"
			>
				<div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
				<p>데이터를 불러오는 중입니다…</p>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<MembersToolbar
				hideGraduated={hideGraduated}
				onToggleHideGraduated={toggleHideGraduated}
				filteredCount={sortedMembers.length}
				totalCount={allMembers.length}
				onOpenAddModal={() => {
					setIsAddModalOpen(true);
				}}
				searchTerm={searchTerm}
				onSearchTermChange={setSearchTerm}
			/>

			<MembersGrid
				visibleMembers={visibleMembers}
				totalCount={sortedMembers.length}
				canLoadMore={visibleCount < sortedMembers.length}
				onLoadMore={() => {
					setVisibleCount((prev) => prev + 48);
				}}
				onAddAlias={handleAddAlias}
				onRemoveAlias={handleRemoveAlias}
				onToggleGraduation={handleToggleGraduation}
				onEditChannel={handleUpdateChannel}
				onEditName={handleEditName}
			/>

			<ConfirmModal
				isOpen={modal.type === "removeAlias"}
				onClose={() => {
					setModal({ type: "none" });
				}}
				onConfirm={confirmRemoveAlias}
				title="별명 삭제"
				message={modal.type === "removeAlias" ? "정말 삭제하시겠습니까?" : ""}
				confirmText="삭제"
				confirmColor="danger"
			>
				{modal.type === "removeAlias" && (
					<div className="mt-2 p-3 bg-slate-50 rounded-lg text-center font-bold text-slate-700">
						{modal.alias}
					</div>
				)}
			</ConfirmModal>

			<ConfirmModal
				isOpen={modal.type === "graduation"}
				onClose={() => {
					setModal({ type: "none" });
				}}
				onConfirm={confirmToggleGraduation}
				title={
					modal.type === "graduation"
						? modal.currentStatus
							? "졸업 해제 (복귀)"
							: "졸업 처리"
						: ""
				}
				message={
					modal.type === "graduation"
						? `${modal.memberName}을(를) ${modal.currentStatus ? "졸업 해제" : "졸업 처리"}하시겠습니까?`
						: ""
				}
				confirmText="확인"
				confirmColor={
					modal.type === "graduation" && modal.currentStatus
						? "primary"
						: "danger"
				}
			/>

			<ChannelEditModal
				isOpen={modal.type === "channelEdit"}
				onClose={() => {
					setModal({ type: "none" });
				}}
				onSave={confirmUpdateChannel}
				memberId={modal.type === "channelEdit" ? modal.memberId : 0}
				memberName={modal.type === "channelEdit" ? modal.memberName : ""}
				currentChannelId={
					modal.type === "channelEdit" ? modal.currentChannelId : ""
				}
			/>

			<EditNameModal
				isOpen={modal.type === "nameEdit"}
				onClose={() => {
					setModal({ type: "none" });
				}}
				onSave={confirmEditName}
				type="member"
				id={modal.type === "nameEdit" ? String(modal.memberId) : ""}
				currentName={modal.type === "nameEdit" ? modal.currentName : ""}
			/>

			<AddMemberModal
				isOpen={isAddModalOpen}
				onClose={() => {
					setIsAddModalOpen(false);
				}}
				onAdd={(data) => {
					const memberData: Partial<Member> = {
						name: data.name,
						channelId: data.channelId,
						nameKo: data.nameKo,
						nameJa: data.nameJa,
						aliases: {
							ko: data.nameKo ? [data.nameKo] : [],
							ja: data.nameJa ? [data.nameJa] : [],
						},
						isGraduated: false,
					};
					mutations.addMember.mutate(memberData);
				}}
			/>
		</div>
	);
};
