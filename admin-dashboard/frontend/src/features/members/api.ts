import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";
import type {
	AddAliasRequest,
	AddMemberRequest,
	Member,
	RemoveAliasRequest,
	SetGraduationRequest,
	UpdateChannelRequest,
} from "./types";

/** 멤버 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const membersApi = {
	getAll: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetMembers({ signal })).data,
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
