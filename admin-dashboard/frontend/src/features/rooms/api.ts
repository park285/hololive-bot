import { holoClient } from "@/api/holoClient";
import type { AddRoomRequest, RemoveRoomRequest, SetACLRequest } from "./types";

export const roomsApi = {
	getAll: holoClient.getRooms,
	add: (request: AddRoomRequest) => holoClient.addRoom(request),
	remove: (request: RemoveRoomRequest) => holoClient.removeRoom(request),
	setACL: (params: SetACLRequest) => holoClient.setAcl(params),
};
