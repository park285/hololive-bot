import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";

/** 통계 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const statsApi = {
	getStatus: async ({ signal }: RequestParams = {}) => (await adminClient.handleAggregatedStatus({ signal })).data,
	get: async ({ signal }: RequestParams = {}) => (await adminClient.holoGetStats({ signal })).data,
};
