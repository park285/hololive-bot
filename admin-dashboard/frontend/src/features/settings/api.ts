import { adminClient } from "@/api/adminClient";
import type { SettingsUpdateResponse } from "@/api/generated/data-contracts";
import type { Settings, SettingsResponse } from "./types";

function isSettingsResponse(data: unknown): data is SettingsResponse {
	if (typeof data !== "object" || data === null || !("status" in data) || data.status !== "ok" ||
		!("settings" in data) || typeof data.settings !== "object" || data.settings === null ||
		!("alarmAdvanceMinutes" in data.settings)) return false;
	const minutes = data.settings.alarmAdvanceMinutes;
	return typeof minutes === "number" && Number.isInteger(minutes) && minutes >= 0 && minutes <= 1440;
}

export const settingsApi = {
	get: async () => {
		const { data } = await adminClient.holoGetSettings();
		if (!isSettingsResponse(data)) throw new Error("설정 조회 응답을 확인하지 못했습니다.");
		return data;
	},
	update: async (settings: Settings) => {
		const { data: body } = await adminClient.holoUpdateSettings(settings);
		const data: unknown = body;
		if (!isSettingsResponse(data) ||
			!("runtime" in data) || typeof data.runtime !== "object" || data.runtime === null || Array.isArray(data.runtime)) {
			throw new Error("설정 저장 응답을 확인하지 못했습니다.");
		}
		return data as SettingsUpdateResponse;
	},
};
