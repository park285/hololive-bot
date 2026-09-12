import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";
import type { StreamOrg } from "./types";

/** 방송 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const streamsApi = {
	getLive: async (org: StreamOrg = "hololive", { signal }: RequestParams = {}) => (await adminClient.holoGetLiveStreams({ org }, { signal })).data,
	getUpcoming: async (org: StreamOrg = "hololive", { signal }: RequestParams = {}) => (await adminClient.holoGetUpcomingStreams({ org }, { signal })).data,
};
