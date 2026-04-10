import { adminClient } from "@/api/adminClient";
import type {
	AddAliasRequest,
	AddMemberRequest,
	AddRoomRequest,
	AlarmsResponse,
	ChannelStatsResponse,
	DeleteAlarmRequest,
	MembersResponse,
	MilestoneStatsResponse,
	MilestonesResponse,
	NearMilestonesResponse,
	RemoveAliasRequest,
	RemoveRoomRequest,
	RoomNameUpdateRequest,
	RoomsResponse,
	SetAclRequest,
	SetAclResponse,
	SetGraduationRequest,
	Settings,
	SettingsResponse,
	StatsResponse,
	StatusOnlyResponse,
	StreamsResponse,
	UpdateChannelRequest,
	UpdateMemberNameRequest,
	UserNameUpdateRequest,
} from "@/api/generated/data-contracts";

export const holoClient = {
	getAlarms: async (): Promise<AlarmsResponse> =>
		(await adminClient.holoGetAlarms()).data,
	deleteAlarm: async (body: DeleteAlarmRequest): Promise<StatusOnlyResponse> =>
		(await adminClient.holoDeleteAlarm(body)).data,
	setRoomName: async (
		body: RoomNameUpdateRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoSetRoomName(body)).data,
	setUserName: async (
		body: UserNameUpdateRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoSetUserName(body)).data,

	getMembers: async (): Promise<MembersResponse> =>
		(await adminClient.holoGetMembers()).data,
	addMember: async (body: AddMemberRequest): Promise<StatusOnlyResponse> =>
		(await adminClient.holoAddMember(body)).data,
	addAlias: async (
		id: number,
		body: AddAliasRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoAddAlias(id, body)).data,
	removeAlias: async (
		id: number,
		body: RemoveAliasRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoRemoveAlias(id, body)).data,
	setGraduation: async (
		id: number,
		body: SetGraduationRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoSetGraduation(id, body)).data,
	updateChannel: async (
		id: number,
		body: UpdateChannelRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoUpdateChannel(id, body)).data,
	updateMemberName: async (
		id: number,
		body: UpdateMemberNameRequest,
	): Promise<StatusOnlyResponse> =>
		(await adminClient.holoUpdateMemberName(id, body)).data,

	getRooms: async (): Promise<RoomsResponse> =>
		(await adminClient.holoGetRooms()).data,
	addRoom: async (body: AddRoomRequest): Promise<StatusOnlyResponse> =>
		(await adminClient.holoAddRoom(body)).data,
	removeRoom: async (body: RemoveRoomRequest): Promise<StatusOnlyResponse> =>
		(await adminClient.holoRemoveRoom(body)).data,
	setAcl: async (body: SetAclRequest): Promise<SetAclResponse> =>
		(await adminClient.holoSetAcl(body)).data,

	getSettings: async (): Promise<SettingsResponse> =>
		(await adminClient.holoGetSettings()).data,
	updateSettings: async (body: Settings): Promise<StatusOnlyResponse> =>
		(await adminClient.holoUpdateSettings(body)).data,

	getStats: async (): Promise<StatsResponse> =>
		(await adminClient.holoGetStats()).data,
	getChannelStats: async (): Promise<ChannelStatsResponse> =>
		(await adminClient.holoGetChannelStats()).data,
	getLiveStreams: async (org = "hololive"): Promise<StreamsResponse> =>
		(await adminClient.holoGetLiveStreams({ org })).data,
	getUpcomingStreams: async (org = "hololive"): Promise<StreamsResponse> =>
		(await adminClient.holoGetUpcomingStreams({ org })).data,

	getMilestones: async (params?: {
		limit?: number;
		offset?: number;
		channelId?: string;
		memberName?: string;
	}): Promise<MilestonesResponse> =>
		(await adminClient.holoGetMilestones(params ?? {})).data,
	getNearMilestones: async (threshold = 0.9): Promise<NearMilestonesResponse> =>
		(await adminClient.holoGetNearMilestones({ threshold })).data,
	getMilestoneStats: async (): Promise<MilestoneStatsResponse> =>
		(await adminClient.holoGetMilestoneStats()).data,
};
