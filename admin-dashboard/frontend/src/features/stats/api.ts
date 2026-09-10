import { adminClient } from "@/app/bootstrap";

export const statsApi = {
	getStatus: async () => (await adminClient.handleAggregatedStatus()).data,
	get: async () => (await adminClient.holoGetStats()).data,
};
