import axios, {
	type AxiosError,
	AxiosHeaders,
	type AxiosInstance,
	type InternalAxiosRequestConfig,
} from "axios";
import { CONFIG } from "@/config/constants";
import { queryClient } from "@/lib/queryClient";
import { useAuthStore } from "@/stores/authStore";

const unsafeMethods = new Set(["post", "put", "delete", "patch"]);

// getCookie: document.cookie에서 특정 쿠키 값을 추출
export function getCookie(name: string): string | null {
	const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	const m = document.cookie.match(new RegExp(`(?:^|; )${escaped}=([^;]*)`));
	return m ? decodeURIComponent(m[1] ?? "") : null;
}

function setRequestHeader(
	config: InternalAxiosRequestConfig,
	name: string,
	value: string,
): void {
	const headers =
		config.headers instanceof AxiosHeaders
			? config.headers
			: AxiosHeaders.from(config.headers);

	headers.set(name, value);
	config.headers = headers;
}

function attachStandardInterceptors(client: AxiosInstance) {
	client.interceptors.request.use((config: InternalAxiosRequestConfig) => {
		if (config.params != null && typeof config.params === "object") {
			const params = config.params as Record<string, unknown>;
			delete params["password"];
			delete params["token"];
		}

		const method = (config.method ?? "get").toLowerCase();
		if (unsafeMethods.has(method)) {
			const token = getCookie("csrf_token");
			if (token != null && token !== "") {
				setRequestHeader(config, "X-CSRF-Token", token);
			}
		}

		return config;
	});

	client.interceptors.response.use(
		(response) => response,
		(error: AxiosError<{ retry_after?: number }>) => {
			if (axios.isAxiosError(error)) {
				if (error.response?.status === 401) {
					useAuthStore.getState().logout();
					queryClient.clear();
					if (window.location.pathname !== "/login") {
						window.location.href = "/login";
					}
				} else if (error.response?.status === 429) {
					const retryAfter =
						error.response.data.retry_after ??
						(typeof error.response.headers["retry-after"] === "string"
							? parseInt(error.response.headers["retry-after"], 10)
							: undefined);
					console.warn(`Rate limited. Retry after ${String(retryAfter)}s`);
				}
			}
			return Promise.reject(error);
		},
	);
}

export function createApiClient(baseURL: string) {
	const client = axios.create({
		baseURL,
		withCredentials: true,
		headers: {
			"Content-Type": "application/json",
		},
		timeout: CONFIG.api.timeoutMs,
	});

	attachStandardInterceptors(client);
	return client;
}

const apiClient = createApiClient(CONFIG.api.baseUrl);

export default apiClient;
