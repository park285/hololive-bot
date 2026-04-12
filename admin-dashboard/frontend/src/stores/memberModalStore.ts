/**
 * 전역 모달 상태 관리 Store
 * 복잡한 Discriminated Union 타입 대신 개별 모달 상태로 분리
 */

import { create } from "zustand";

/** 별명 삭제 모달 데이터 */
interface AliasRemovalData {
	memberId: number;
	aliasType: "ko" | "ja";
	alias: string;
}

/** 졸업 상태 변경 모달 데이터 */
interface GraduationData {
	memberId: number;
	memberName: string;
	currentStatus: boolean;
}

/** 채널 ID 수정 모달 데이터 */
interface ChannelEditData {
	memberId: number;
	memberName: string;
	currentChannelId: string;
}

/** 이름 수정 모달 데이터 */
interface NameEditData {
	memberId: number;
	currentName: string;
}

interface MemberModalStore {
	aliasRemoval: AliasRemovalData | null;
	graduation: GraduationData | null;
	channelEdit: ChannelEditData | null;
	nameEdit: NameEditData | null;

	openAliasRemoval: (data: AliasRemovalData) => void;
	closeAliasRemoval: () => void;

	openGraduation: (data: GraduationData) => void;
	closeGraduation: () => void;

	openChannelEdit: (data: ChannelEditData) => void;
	closeChannelEdit: () => void;

	openNameEdit: (data: NameEditData) => void;
	closeNameEdit: () => void;

	closeAll: () => void;
}

export const useMemberModalStore = create<MemberModalStore>()((set) => ({
	aliasRemoval: null,
	graduation: null,
	channelEdit: null,
	nameEdit: null,

	openAliasRemoval: (data) => {
		set({ aliasRemoval: data });
	},
	closeAliasRemoval: () => {
		set({ aliasRemoval: null });
	},

	openGraduation: (data) => {
		set({ graduation: data });
	},
	closeGraduation: () => {
		set({ graduation: null });
	},

	openChannelEdit: (data) => {
		set({ channelEdit: data });
	},
	closeChannelEdit: () => {
		set({ channelEdit: null });
	},

	openNameEdit: (data) => {
		set({ nameEdit: data });
	},
	closeNameEdit: () => {
		set({ nameEdit: null });
	},

	closeAll: () => {
		set({
			aliasRemoval: null,
			graduation: null,
			channelEdit: null,
			nameEdit: null,
		});
	},
}));

export type { AliasRemovalData, ChannelEditData, GraduationData, NameEditData };
