import { adminClient } from "@/api/adminClient";
import type { AddRoomRequest, RemoveRoomRequest, SetACLRequest } from "./types";

export const roomsApi = {
	getAll: async () => (await adminClient.holoGetRooms()).data,
	getJoined: async () => (await adminClient.holoGetRoomsJoined()).data,
	add: async (request: AddRoomRequest) =>
		(await adminClient.holoAddRoom(request)).data,
	remove: async (request: RemoveRoomRequest) =>
		(await adminClient.holoRemoveRoom(request)).data,
	setACL: async (params: SetACLRequest) =>
		(await adminClient.holoSetAcl(params)).data,
};
