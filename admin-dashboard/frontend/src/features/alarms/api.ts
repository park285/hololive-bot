import type { DeleteAlarmRequest } from "@/api/generated/data-contracts";
import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";

/** 알람 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const alarmsApi = {
	getAll: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetAlarms({ signal })).data,
	delete: async (request: DeleteAlarmRequest) =>
		(await adminClient.holoDeleteAlarm(request)).data,
};

export const namesApi = {
	setRoomName: async (roomId: string, roomName: string) =>
		(await adminClient.holoSetRoomName({ roomId, roomName })).data,
	setUserName: async (userId: string, userName: string) =>
		(await adminClient.holoSetUserName({ userId, userName })).data,
};
