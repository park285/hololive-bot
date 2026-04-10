import { holoClient } from "@/api/holoClient";
import type { Settings } from "./types";

export const settingsApi = {
	get: holoClient.getSettings,
	update: (settings: Settings) => holoClient.updateSettings(settings),
};
