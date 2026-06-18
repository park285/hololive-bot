import { isAxiosError } from "axios";
import type {
	AggregatedStatus as GeneratedAggregatedStatus,
	Container as GeneratedContainer,
	DockerContainerListResponse as GeneratedDockerContainerListResponse,
} from "@/api/generated/data-contracts";
import apiClient, { clearCSRFToken, setCSRFToken } from "./client";

export interface HeartbeatResponse {
	status?: string;
	rotated?: boolean | null;
	absolute_expires_at?: number | null;
	idle_rejected?: boolean | null;
	absolute_expired?: boolean | null;
	csrf_token?: string | null;
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
	message?: string | null;
}

interface AuthStatusResponse {
	status?: string;
	message?: string | null;
}

interface LoginStatusResponse extends AuthStatusResponse {
	csrf_token?: string | null;
}

type DockerContainerApiResponse = GeneratedContainer &
	Partial<{
		managed: boolean;
		paused: boolean;
		startedAt: string;
		cpuPercent: number;
		memoryUsageMB: number;
		memoryLimitMB: number;
		memoryPercent: number;
		networkRxMB: number;
		networkTxMB: number;
		blockReadMB: number;
		blockWriteMB: number;
		goroutineCount: number;
	}>;

const normalizeStatusOnly = (
	data: Partial<StatusOnlyResponse> | null | undefined,
): StatusOnlyResponse => ({
	status: data?.status ?? "ok",
	message: data?.message,
});

const mapDockerContainer = (container: DockerContainerApiResponse): DockerContainer => ({
	id: container.id,
	name: container.name,
	state: container.state,
	status: container.status,
	image: container.image,
	health: container.health ?? "none",
	managed: container.managed ?? false,
	paused: container.paused ?? container.state === "paused",
	created: container.created,
	ports: container.ports,
	...(container.startedAt !== undefined ? { startedAt: container.startedAt } : {}),
	...(container.cpuPercent !== undefined
		? { cpuPercent: container.cpuPercent }
		: {}),
	...(container.memoryUsageMB !== undefined
		? { memoryUsageMB: container.memoryUsageMB }
		: {}),
	...(container.memoryLimitMB !== undefined
		? { memoryLimitMB: container.memoryLimitMB }
		: {}),
	...(container.memoryPercent !== undefined
		? { memoryPercent: container.memoryPercent }
		: {}),
	...(container.networkRxMB !== undefined
		? { networkRxMB: container.networkRxMB }
		: {}),
	...(container.networkTxMB !== undefined
		? { networkTxMB: container.networkTxMB }
		: {}),
	...(container.blockReadMB !== undefined
		? { blockReadMB: container.blockReadMB }
		: {}),
	...(container.blockWriteMB !== undefined
		? { blockWriteMB: container.blockWriteMB }
		: {}),
	...(container.goroutineCount !== undefined
		? { goroutineCount: container.goroutineCount }
		: {}),
});

export const authApi = {
	login: async (username: string, password: string): Promise<LoginStatusResponse> => {
		const { data } = await apiClient.post<
			Partial<LoginStatusResponse> | undefined
		>("/auth/login", {
			username,
			password,
		});

		if (data?.status !== "ok") {
			throw new Error(data?.message ?? "인증에 실패했습니다.");
		}

		setCSRFToken(data.csrf_token);
		return data;
	},

	logout: async (): Promise<StatusOnlyResponse> => {
		const { data } = await apiClient.post<
			Partial<AuthStatusResponse> | undefined
		>(
			"/auth/logout",
		);
		clearCSRFToken();
		return normalizeStatusOnly(data);
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
			const response = await apiClient.post<HeartbeatResponse>(
				"/auth/heartbeat",
				{ idle },
				{ signal },
			);
			if (response.data.csrf_token !== undefined) {
				setCSRFToken(response.data.csrf_token);
			}
			return response.data;
		} catch (error) {
			if (isAxiosError(error) && error.response?.data) {
				const response = error.response.data as HeartbeatResponse;
				if (response.csrf_token !== undefined) {
					setCSRFToken(response.csrf_token);
				}
				return response;
			}
			throw error;
		}
	},
};

const postDockerAction = async (
	name: string,
	action: "restart" | "stop" | "start",
): Promise<StatusOnlyResponse> => {
	const { data } = await apiClient.post<Partial<StatusOnlyResponse> | undefined>(
		`/docker/containers/${encodeURIComponent(name)}/${action}`,
	);
	return normalizeStatusOnly(data);
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
		const containers = (data.containers as DockerContainerApiResponse[]).map(
			mapDockerContainer,
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
