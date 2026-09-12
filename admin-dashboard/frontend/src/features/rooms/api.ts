import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";
import type { AddRoomRequest, RemoveRoomRequest, SetACLRequest } from "./types";

/** 방 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const roomsApi = {
	getAll: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetRooms({ signal })).data,
	getJoined: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetRoomsJoined({ signal })).data,
	add: async (request: AddRoomRequest) =>
		(await adminClient.holoAddRoom(request)).data,
	remove: async (request: RemoveRoomRequest) =>
		(await adminClient.holoRemoveRoom(request)).data,
	setACL: async (params: SetACLRequest) =>
		(await adminClient.holoSetAcl(params)).data,
};
