import { create } from "zustand";

interface AliasRemovalData {
	memberId: number;
	aliasType: "ko" | "ja";
	alias: string;
}

interface GraduationData {
	memberId: number;
	memberName: string;
	currentStatus: boolean;
}

interface ChannelEditData {
	memberId: number;
	memberName: string;
	currentChannelId: string;
}

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
