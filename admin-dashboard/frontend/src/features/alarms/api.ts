import type { DeleteAlarmRequest } from "@/api/generated/data-contracts";
import { holoClient } from "@/api/holoClient";

export const alarmsApi = {
	getAll: holoClient.getAlarms,
	delete: (request: DeleteAlarmRequest) => holoClient.deleteAlarm(request),
};

export const namesApi = {
	setRoomName: (roomId: string, roomName: string) =>
		holoClient.setRoomName({ roomId, roomName }),
	setUserName: (userId: string, userName: string) =>
		holoClient.setUserName({ userId, userName }),
};
