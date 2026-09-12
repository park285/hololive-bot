import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";

/** Docker 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const dockerApi = {
	checkHealth: async ({ signal }: RequestParams = {}) => (await adminClient.handleDockerHealth({ signal })).data,
	getContainers: async ({ signal }: RequestParams = {}) => (await adminClient.handleDockerContainers({ signal })).data,
	restartContainer: async (name: string) => (await adminClient.handleDockerRestart(name)).data,
	stopContainer: async (name: string) => (await adminClient.handleDockerStop(name)).data,
	startContainer: async (name: string) => (await adminClient.handleDockerStart(name)).data,
};
