import { adminClient } from "@/app/bootstrap";

export const dockerApi = {
	checkHealth: async () => (await adminClient.handleDockerHealth()).data,
	getContainers: async () => (await adminClient.handleDockerContainers()).data,
	restartContainer: async (name: string) => (await adminClient.handleDockerRestart(name)).data,
	stopContainer: async (name: string) => (await adminClient.handleDockerStop(name)).data,
	startContainer: async (name: string) => (await adminClient.handleDockerStart(name)).data,
};
