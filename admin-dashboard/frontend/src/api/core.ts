import { isAxiosError } from "axios";
import type {
	AggregatedStatus as GeneratedAggregatedStatus,
	Container as GeneratedContainer,
	DockerContainerListResponse as GeneratedDockerContainerListResponse,
	SessionStatusResponse as GeneratedSessionStatusResponse,
	StatusOnlyResponse as GeneratedStatusOnlyResponse,
} from "@/api/generated/data-contracts";
import { broadcastSessionRefresh } from "@/hooks/useActivityDetection";
import apiClient, { clearCSRFToken, getCSRFVersion, setCSRFToken } from "./client";

export interface HeartbeatResponse {
	status?: string;
	rotated?: boolean | null;
	absolute_expires_at?: number | null;
	idle_rejected?: boolean | null;
	absolute_expired?: boolean | null;
	csrf_token?: string | null;
	error?: string;
}

export type SessionStatusResponse = GeneratedSessionStatusResponse;

export interface DockerContainer {
	id: string;
	name: string;
	state: string;
	status: string;
	image: string;
	health: string;
	managed: boolean;
	stopBlocked: boolean;
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

export type StatusOnlyResponse = GeneratedStatusOnlyResponse;

interface AuthStatusResponse {
	status?: string;
	message?: string | null;
}

interface LoginStatusResponse extends AuthStatusResponse {
	csrf_token?: string | null;
}

function parseHeartbeatResponse(data: unknown, status: number): HeartbeatResponse {
	if (typeof data !== "object" || data === null || Array.isArray(data)) {
		throw new Error("세션 확인 응답이 올바르지 않습니다.");
	}
	const value = data as { [K in keyof HeartbeatResponse]: unknown };
	if (status === 401 && (value.error === "Unauthorized" || value.error === "Session expired") &&
		(value.absolute_expired === undefined || typeof value.absolute_expired === "boolean")) {
		return { error: value.error, absolute_expired: value.absolute_expired };
	}
	if (status === 200 && value.status === "idle" && value.idle_rejected === true && value.error === undefined) {
		return { status: "idle", idle_rejected: true };
	}
	if (status !== 200 || value.status !== "ok" || value.error !== undefined ||
		value.idle_rejected === true || value.absolute_expired === true ||
		typeof value.absolute_expires_at !== "number" || !Number.isFinite(value.absolute_expires_at) ||
		(value.rotated !== undefined && typeof value.rotated !== "boolean") ||
		(value.csrf_token !== undefined && (typeof value.csrf_token !== "string" || value.csrf_token.length === 0)) ||
		(value.rotated === true && typeof value.csrf_token !== "string")) {
		throw new Error("세션 확인 응답이 올바르지 않습니다.");
	}
	return {
		status: "ok",
		absolute_expires_at: value.absolute_expires_at,
		rotated: value.rotated,
		csrf_token: value.csrf_token,
	};
}

type DockerContainerApiResponse = GeneratedContainer;

const mapDockerContainer = (container: DockerContainerApiResponse): DockerContainer => ({
	id: container.id,
	name: container.name,
	state: container.state,
	status: container.status,
	image: container.image,
	health: container.health ?? "none",
	managed: container.managed,
	stopBlocked: container.stopBlocked,
	created: container.created,
	ports: container.ports,
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
		try {
			const { data, status } = await apiClient.post<unknown>("/auth/logout");
			if (status !== 200 || typeof data !== "object" || data === null ||
				!("status" in data) || data.status !== "ok") {
				throw new Error("로그아웃 응답이 올바르지 않습니다.");
			}
			return { status: "ok" };
		} catch (error) {
			throw new Error("이 브라우저의 세션은 정리했지만 서버 세션 폐기를 확인하지 못했습니다.", { cause: error });
		} finally {
			clearCSRFToken();
		}
	},

	getSession: async (signal?: AbortSignal): Promise<SessionStatusResponse> => {
		const version = getCSRFVersion();
		const { data: body } = await apiClient.get<unknown>("/auth/session", { signal });
		signal?.throwIfAborted();
		if (typeof body !== "object" || body === null || Array.isArray(body)) throw new Error("세션 조회 응답이 올바르지 않습니다.");
		const data = body as { [K in keyof SessionStatusResponse]: unknown };
		if (data.status !== "ok" ||
			data.authenticated !== true || typeof data.csrf_token !== "string" || data.csrf_token.length === 0 ||
			typeof data.absolute_expires_at !== "number" || !Number.isFinite(data.absolute_expires_at) ||
			typeof data.session_policy !== "object" || data.session_policy === null) {
			throw new Error("세션 조회 응답이 올바르지 않습니다.");
		}
		if (!setCSRFToken(data.csrf_token, version)) {
			throw new DOMException("조회 중 세션이 변경되었습니다.", "AbortError");
		}
		return data as SessionStatusResponse;
	},

	heartbeat: async (
		idle = false,
		signal?: AbortSignal,
	): Promise<HeartbeatResponse> => {
		try {
			const response = await apiClient.post<unknown>(
				"/auth/heartbeat",
				{ idle },
				{ signal },
			);
			const parsed = parseHeartbeatResponse(response.data, response.status);
			if (parsed.csrf_token !== undefined) {
				// 늦은 회전 응답의 token을 다른 탭에 덮어쓰지 않고 현재 공유 cookie로 조회한다.
				clearCSRFToken();
				broadcastSessionRefresh();
				const session = await authApi.getSession(signal);
				parsed.csrf_token = session.csrf_token;
			}
			return parsed;
		} catch (error) {
			if (isAxiosError(error) && error.response?.status === 401) {
				return parseHeartbeatResponse(error.response.data, 401);
			}
			throw error;
		}
	},
};

export class DockerActionOutcomeUnknownError extends Error {
	constructor(cause?: unknown) {
		super("작업 실행 결과를 확인하지 못했습니다. 자동 재시도하지 않았습니다. 현재 상태를 확인해 주세요.", { cause });
		this.name = "DockerActionOutcomeUnknownError";
	}
}

function validateDockerActionResponse(data: unknown): StatusOnlyResponse {
	if (
		typeof data !== "object" || data === null || Array.isArray(data) ||
		!("status" in data) || data.status !== "ok"
	) {
		throw new DockerActionOutcomeUnknownError();
	}
	const message = "message" in data ? data.message : undefined;
	if (message !== undefined && message !== null && typeof message !== "string") {
		throw new DockerActionOutcomeUnknownError();
	}
	return { status: "ok", message };
}

const postDockerAction = async (
	name: string,
	action: "restart" | "stop" | "start",
): Promise<StatusOnlyResponse> => {
	try {
		const { data, status } = await apiClient.post<unknown>(
			`/docker/containers/${encodeURIComponent(name)}/${action}`,
		);
		if (status !== 200) {
			throw new DockerActionOutcomeUnknownError();
		}
		return validateDockerActionResponse(data);
	} catch (error) {
		if (error instanceof DockerActionOutcomeUnknownError) {
			throw error;
		}
		if (!isAxiosError(error) || !error.response || error.response.status >= 500) {
			throw new DockerActionOutcomeUnknownError(error);
		}
		throw error;
	}
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
		const containers = data.containers.map(mapDockerContainer);
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
