import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";
import type { Settings } from "./types";

/** 설정 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const settingsApi = {
	get: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetSettings({ signal })).data,
	update: async (settings: Settings) => (await adminClient.holoUpdateSettings(settings)).data,
};
