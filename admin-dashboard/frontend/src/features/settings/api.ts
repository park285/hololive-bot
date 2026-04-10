import { adminClient } from "@/api/adminClient";
import type { Settings } from "./types";

export const settingsApi = {
	get: async () => (await adminClient.holoGetSettings()).data,
	update: async (settings: Settings) =>
		(await adminClient.holoUpdateSettings(settings)).data,
};
