import { isAxiosError } from "axios";
import type {
	AggregatedStatus as GeneratedAggregatedStatus,
	Container as GeneratedContainer,
	DockerContainerListResponse as GeneratedDockerContainerListResponse,
} from "@/api/generated/data-contracts";
import apiClient from "./client";

export interface HeartbeatResponse {
	status?: string;
	rotated?: boolean;
	absolute_expires_at?: number;
	idle_rejected?: boolean;
	absolute_expired?: boolean;
	error?: string;
}

export interface SessionStatusResponse {
	status: string;
	authenticated: boolean;
	username: string;
	absolute_expires_at: number;
	session_policy: {
		heartbeat_interval_ms: number;
		idle_timeout_ms: number;
		idle_warning_timeout_ms: number;
		idle_session_ttl_ms: number;
		absolute_warning_window_ms: number;
	};
}

export interface DockerContainer {
	id: string;
	name: string;
	state: string;
	status: string;
	image: string;
	health: string;
	managed: boolean;
	paused: boolean;
	startedAt?: string;
	cpuPercent?: number;
	memoryUsageMB?: number;
	memoryLimitMB?: number;
	memoryPercent?: number;
	networkRxMB?: number;
	networkTxMB?: number;
	blockReadMB?: number;
	blockWriteMB?: number;
	goroutineCount?: number;
	created: number;
	ports: GeneratedContainer["ports"];
}

export interface DockerHealthResponse {
	status: string;
	available: boolean;
}

export interface DockerContainersResponse {
	status: string;
	containers: DockerContainer[];
}

export interface StatusOnlyResponse {
	status: string;
	message?: string;
}

interface AuthStatusResponse {
	status?: string;
	message?: string;
}

export const authApi = {
	login: async (username: string, password: string): Promise<void> => {
		const response = await apiClient.post<AuthStatusResponse>("/auth/login", {
			username,
			password,
		});
		if (response.data.status !== "ok") {
			throw new Error(response.data.message || "인증에 실패했습니다.");
		}
	},

	logout: async (): Promise<StatusOnlyResponse> => {
		const response = await apiClient.post<AuthStatusResponse>("/auth/logout");
		return {
			status: response.data.status ?? "ok",
			message: response.data.message,
		};
	},

	getSession: async (): Promise<SessionStatusResponse> => {
		const { data } = await apiClient.get<SessionStatusResponse>("/auth/session");
		return data;
	},

	heartbeat: async (
		idle = false,
		signal?: AbortSignal,
	): Promise<HeartbeatResponse> => {
		try {
			const response = await apiClient.post(
				"/auth/heartbeat",
				{ idle },
				{ signal },
			);
			return response.data as HeartbeatResponse;
		} catch (error) {
			if (isAxiosError(error) && error.response?.data) {
				return error.response.data as HeartbeatResponse;
			}
			throw error;
		}
	},
};

const postDockerAction = async (
	name: string,
	action: "restart" | "stop" | "start",
): Promise<StatusOnlyResponse> => {
	const { data } = await apiClient.post<StatusOnlyResponse>(
		`/docker/containers/${encodeURIComponent(name)}/${action}`,
	);
	return data;
};

export const dockerApi = {
	checkHealth: async (): Promise<DockerHealthResponse> => {
		const { data } = await apiClient.get<DockerHealthResponse>("/docker/health");
		return data;
	},

	getContainers: async (): Promise<DockerContainersResponse> => {
		const { data } = await apiClient.get<GeneratedDockerContainerListResponse>(
			"/docker/containers",
		);
		const containers: DockerContainer[] = data.containers.map(
			(c: GeneratedContainer) => ({
				id: c.id,
				name: c.name,
				state: c.state,
				status: c.status,
				image: c.image,
				health: c.health ?? "none",
				managed: true,
				paused: false,
				created: c.created,
				ports: c.ports,
			}),
		);
		return { status: data.status, containers };
	},

	restartContainer: (name: string) => postDockerAction(name, "restart"),

	stopContainer: (name: string) => postDockerAction(name, "stop"),

	startContainer: (name: string) => postDockerAction(name, "start"),
};

export interface ServiceStatus {
	name: string;
	available: boolean;
	response_time_ms?: number | null;
	error?: string | null;
}

export type AggregatedStatus = GeneratedAggregatedStatus;

export const statusApi = {
	get: async (): Promise<AggregatedStatus> => {
		const { data } = await apiClient.get<AggregatedStatus>("/status");
		return data;
	},
};
