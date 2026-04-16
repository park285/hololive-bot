import { lazy, Suspense } from "react";
import { MembersGrid } from "@/features/members/components/MembersGrid";
import { MembersToolbar } from "@/features/members/components/MembersToolbar";
import { useMembersPage } from "@/features/members/hooks/useMembersPage";
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

	const preloadAddMemberModal = () => {
		void loadAddMemberModal();
	};

	const openAddModal = () => {
		preloadAddMemberModal();
		setIsAddModalOpen(true);
	};

	const openRemoveAliasModal = (
		memberId: number,
		type: "ko" | "ja",
		alias: string,
	) => {
		void loadConfirmModal();
		handleRemoveAlias(memberId, type, alias);
	};

	const openGraduationModal = (
		memberId: number,
		memberName: string,
		currentStatus: boolean,
	) => {
		void loadConfirmModal();
		handleToggleGraduation(memberId, memberName, currentStatus);
	};

	const openChannelEditModal = (
		memberId: number,
		memberName: string,
		currentChannelId: string,
	) => {
		void loadChannelEditModal();
		handleUpdateChannel(memberId, memberName, currentChannelId);
	};

	const openNameEditModal = (memberId: number, currentName: string) => {
		void loadEditNameModal();
		handleEditName(memberId, currentName);
	};

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
				onAddModalIntent={preloadAddMemberModal}
				onOpenAddModal={openAddModal}
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
				onRemoveAlias={openRemoveAliasModal}
				onToggleGraduation={openGraduationModal}
				onEditChannel={openChannelEditModal}
				onEditName={openNameEditModal}
			/>

			{modal.type === "removeAlias" && (
				<Suspense fallback={null}>
					<ConfirmModal
						isOpen
						onClose={() => {
							setModal({ type: "none" });
						}}
						onConfirm={confirmRemoveAlias}
						title="별명 삭제"
						message="정말 삭제하시겠습니까?"
						confirmText="삭제"
						confirmColor="danger"
					>
						<div className="mt-2 rounded-lg bg-slate-50 p-3 text-center font-bold text-slate-700">
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
						currentChannelId={modal.currentChannelId}
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
						id={String(modal.memberId)}
						currentName={modal.currentName}
					/>
				</Suspense>
			)}

			{isAddModalOpen && (
				<Suspense fallback={null}>
					<AddMemberModal
						isOpen
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
				</Suspense>
			)}
		</div>
	);
};
