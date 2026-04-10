import { adminClient } from "@/api/adminClient";

export const statsApi = {
	get: async () => (await adminClient.holoGetStats()).data,
	getChannels: async () => (await adminClient.holoGetChannelStats()).data,
};
