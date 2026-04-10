import type { DeleteAlarmRequest } from "@/api/generated/data-contracts";
import { adminClient } from "@/api/adminClient";

export const alarmsApi = {
	getAll: async () => (await adminClient.holoGetAlarms()).data,
	delete: async (request: DeleteAlarmRequest) =>
		(await adminClient.holoDeleteAlarm(request)).data,
};

export const namesApi = {
	setRoomName: async (roomId: string, roomName: string) =>
		(await adminClient.holoSetRoomName({ roomId, roomName })).data,
	setUserName: async (userId: string, userName: string) =>
		(await adminClient.holoSetUserName({ userId, userName })).data,
};
