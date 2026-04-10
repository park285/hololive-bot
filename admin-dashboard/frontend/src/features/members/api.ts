import { holoClient } from "@/api/holoClient";
import type {
	AddAliasRequest,
	AddMemberRequest,
	Member,
	RemoveAliasRequest,
	SetGraduationRequest,
	UpdateChannelRequest,
} from "./types";

export const membersApi = {
	getAll: holoClient.getMembers,
	add: async (member: Partial<Member>) => {
		const request: AddMemberRequest = {
			name: member.name ?? "",
			channelId: member.channelId ?? "",
			aliases: member.aliases ?? { ko: [], ja: [] },
			nameJa: member.nameJa,
			nameKo: member.nameKo,
			isGraduated: member.isGraduated ?? false,
		};
		return holoClient.addMember(request);
	},
	addAlias: (memberId: number, request: AddAliasRequest) =>
		holoClient.addAlias(memberId, request),
	removeAlias: (memberId: number, request: RemoveAliasRequest) =>
		holoClient.removeAlias(memberId, request),
	setGraduation: (memberId: number, request: SetGraduationRequest) =>
		holoClient.setGraduation(memberId, request),
	updateChannel: (memberId: number, request: UpdateChannelRequest) =>
		holoClient.updateChannel(memberId, request),
	updateName: (memberId: number, name: string) =>
		holoClient.updateMemberName(memberId, { name }),
};
