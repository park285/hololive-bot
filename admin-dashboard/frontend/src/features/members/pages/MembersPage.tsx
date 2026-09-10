import { lazy, Suspense } from "react";
import { MembersGrid } from "@/features/members/components/MembersGrid";
import { MembersToolbar } from "@/features/members/components/MembersToolbar";
import { useMembersPage } from "@/features/members/hooks/useMembersPage";
import { QueryNotice } from "@/queries/QueryNotice";
import type { Member } from "@/features/members/types";

const loadAddMemberModal = () => import("@/components/AddMemberModal");
const loadChannelEditModal = () => import("@/components/ChannelEditModal");
const loadEditNameModal = () => import("@/components/EditNameModal");
const loadConfirmModal = () =>
	import("@/components/ConfirmModal").then((module) => ({
		default: module.ConfirmModal,
	}));

const AddMemberModal = lazy(loadAddMemberModal);
const ChannelEditModal = lazy(loadChannelEditModal);
const EditNameModal = lazy(loadEditNameModal);
const ConfirmModal = lazy(loadConfirmModal);

export const MembersPage = () => {
	const {
		query,
		view,
		mutations,
		searchTerm,
		setSearchTerm,
		hideGraduated,
		toggleHideGraduated,
		modal,
		setModal,
		addModal,
		setAddModal,
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

	const preloadAddMemberModal = () => {
		void loadAddMemberModal();
	};

	const openAddModal = () => {
		preloadAddMemberModal();
		setAddModal({});
	};

	const openRemoveAliasModal = (
		memberId: string,
		type: "ko" | "ja",
		alias: string,
	) => {
		void loadConfirmModal();
		handleRemoveAlias(memberId, type, alias);
	};

	const openGraduationModal = (
		memberId: string,
		memberName: string,
		currentStatus: boolean,
	) => {
		void loadConfirmModal();
		handleToggleGraduation(memberId, memberName, currentStatus);
	};

	const openChannelEditModal = (
		memberId: string,
		memberName: string,
		currentChannelId: string,
	) => {
		void loadChannelEditModal();
		handleUpdateChannel(memberId, memberName, currentChannelId);
	};

	const openNameEditModal = (memberId: string, currentName: string) => {
		void loadEditNameModal();
		handleEditName(memberId, currentName);
	};

	const readState = <QueryNotice view={view} label="멤버 목록" onRetry={() => { void query.refetch(); }} />;
	const editedMember = modal.type !== "none" ? allMembers.find(member => member.id === modal.memberId) : undefined;
	if (view.data === undefined) return readState;

	return (
		<div className="space-y-6">
			{readState}
			<MembersToolbar
				canAdd={view.current}
				hideGraduated={hideGraduated}
				onToggleHideGraduated={toggleHideGraduated}
				filteredCount={sortedMembers.length}
				totalCount={allMembers.length}
				onAddModalIntent={preloadAddMemberModal}
				onOpenAddModal={openAddModal}
				searchTerm={searchTerm}
				onSearchTermChange={setSearchTerm}
			/>

			<fieldset disabled={!view.current} className="min-w-0">
			<MembersGrid
				visibleMembers={visibleMembers}
				totalCount={sortedMembers.length}
				canLoadMore={visibleCount < sortedMembers.length}
				onLoadMore={() => {
					setVisibleCount((prev) => prev + 48);
				}}
				onAddAlias={handleAddAlias}
				onRemoveAlias={openRemoveAliasModal}
				onToggleGraduation={openGraduationModal}
				onEditChannel={openChannelEditModal}
				onEditName={openNameEditModal}
			/>

			</fieldset>

			{modal.type === "removeAlias" && (
				<Suspense fallback={null}>
					<ConfirmModal
						isOpen
						onClose={() => {
							setModal({ type: "none" });
						}}
						onConfirm={confirmRemoveAlias}
						canConfirm={view.current}
						isPending={mutations.removeAlias.isPending}
						title="별명 삭제"
						message="정말 삭제하시겠습니까?"
						confirmText="삭제"
						confirmColor="danger"
					>
						<div className="mt-2 rounded-lg bg-muted p-3 text-center font-bold text-foreground">
							{modal.alias}
						</div>
					</ConfirmModal>
				</Suspense>
			)}

			{modal.type === "graduation" && (
				<Suspense fallback={null}>
					<ConfirmModal
						isOpen
						onClose={() => {
							setModal({ type: "none" });
						}}
						onConfirm={confirmToggleGraduation}
						canConfirm={view.current && editedMember?.isGraduated === modal.currentStatus}
						isPending={mutations.setGraduation.isPending}
						title={modal.currentStatus ? "졸업 해제 (복귀)" : "졸업 처리"}
						message={`${modal.memberName}을(를) ${modal.currentStatus ? "졸업 해제" : "졸업 처리"}하시겠습니까?`}
						confirmText="확인"
						confirmColor={modal.currentStatus ? "primary" : "danger"}
					/>
				</Suspense>
			)}

			{modal.type === "channelEdit" && (
				<Suspense fallback={null}>
					<ChannelEditModal
						isOpen
						onClose={() => {
							setModal({ type: "none" });
						}}
						onSave={confirmUpdateChannel}
						memberId={modal.memberId}
						memberName={modal.memberName}
						key={modal.memberId}
						currentChannelId={editedMember?.channelId}
						canSubmit={view.current && editedMember !== undefined}
						pending={mutations.updateChannel.isPending}
						readState={readState}
					/>
				</Suspense>
			)}

			{modal.type === "nameEdit" && (
				<Suspense fallback={null}>
					<EditNameModal
						isOpen
						onClose={() => {
							setModal({ type: "none" });
						}}
						onSave={confirmEditName}
						type="member"
						id={modal.memberId}
						key={modal.memberId}
						currentName={editedMember?.name}
						canSubmit={view.current && editedMember !== undefined}
						pending={mutations.updateName.isPending}
						readState={readState}
					/>
				</Suspense>
			)}

			{addModal && (
				<Suspense fallback={null}>
					<AddMemberModal
						canSubmit={view.current}
						pending={mutations.addMember.isPending}
						readState={readState}
						isOpen
						onClose={() => {
							setAddModal(null);
						}}
						onAdd={async (data) => {
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
							await mutations.addMember.mutateAsync(memberData);
							setAddModal(current => current === addModal ? null : current);
						}}
					/>
				</Suspense>
			)}
		</div>
	);
};
