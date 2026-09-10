import { adminClient } from "@/app/bootstrap";
import type { StreamOrg } from "./types";

export const streamsApi = {
	getLive: async (org: StreamOrg = "hololive") => (await adminClient.holoGetLiveStreams({ org })).data,
	getUpcoming: async (org: StreamOrg = "hololive") => (await adminClient.holoGetUpcomingStreams({ org })).data,
};
