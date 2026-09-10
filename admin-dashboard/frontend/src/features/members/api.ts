import { adminClient } from "@/app/bootstrap";
import type {
	AddAliasRequest,
	AddMemberRequest,
	Member,
	RemoveAliasRequest,
	SetGraduationRequest,
	UpdateChannelRequest,
} from "./types";

export const membersApi = {
	getAll: async () => (await adminClient.holoGetMembers()).data,
	add: async (member: Partial<Member>) => {
		const request: AddMemberRequest = {
			name: member.name ?? "",
			channelId: member.channelId ?? "",
			aliases: member.aliases ?? { ko: [], ja: [] },
			nameJa: member.nameJa,
			nameKo: member.nameKo,
			isGraduated: member.isGraduated ?? false,
		};
		return (await adminClient.holoAddMember(request)).data;
	},
	addAlias: async (memberId: string, request: AddAliasRequest) =>
		(await adminClient.holoAddAlias(memberId, request)).data,
	removeAlias: async (memberId: string, request: RemoveAliasRequest) =>
		(await adminClient.holoRemoveAlias(memberId, request)).data,
	setGraduation: async (memberId: string, request: SetGraduationRequest) =>
		(await adminClient.holoSetGraduation(memberId, request)).data,
	updateChannel: async (memberId: string, request: UpdateChannelRequest) =>
		(await adminClient.holoUpdateChannel(memberId, request)).data,
	updateName: async (memberId: string, name: string) =>
		(await adminClient.holoUpdateMemberName(memberId, { name })).data,
};
