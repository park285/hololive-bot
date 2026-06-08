import { isAxiosError } from "axios";

export function extractErrorMessage(data: unknown): string | undefined {
	if (typeof data === "object" && data !== null && "message" in data) {
		const { message } = data;
		return typeof message === "string" ? message : String(message);
	}
	if (typeof data === "object" && data !== null && "error" in data) {
		const { error } = data;
		return typeof error === "string" ? error : String(error);
	}
	return undefined;
}

export function extractStringProperty(
	data: unknown,
	key: string,
): string | undefined {
	if (typeof data === "object" && data !== null && key in data) {
		const value = (data as Record<string, unknown>)[key];
		return typeof value === "string" ? value : undefined;
	}
	return undefined;
}

export function hasProperty<K extends string>(
	data: unknown,
	key: K,
): data is Record<K, unknown> {
	return typeof data === "object" && data !== null && key in data;
}

export function getErrorMessageFromUnknown(error: unknown): string {
	if (isAxiosError(error)) {
		if (error.response?.status === 401) return "세션이 만료되었거나 접근 권한이 없습니다. 다시 로그인해주세요.";
		if (error.response?.status === 403) return "해당 기능에 대한 권한이 없습니다.";
		if (error.response?.status === 404) return "요청한 데이터를 찾을 수 없습니다.";
		if (error.response?.status === 429) return "요청이 너무 많습니다. 잠시 후 다시 시도해주세요.";
		if (error.response?.status && error.response.status >= 500) return "서버에 오류가 발생했습니다. 잠시 후 다시 시도해주세요.";
		
		const extracted = extractErrorMessage(error.response?.data);
		if (extracted) {
			if (extracted === "Session expired" || extracted === "Unauthorized") return "세션이 만료되었거나 접근 권한이 없습니다.";
			return extracted;
		}
	}

	if (error instanceof Error) {
		if (error.message === "Network Error") return "네트워크 연결 오류가 발생했습니다.";
		if (error.message === "Session expired" || error.message === "Unauthorized") return "세션이 만료되었거나 접근 권한이 없습니다.";
		return error.message;
	}
	if (typeof error === "string") {
		if (error === "Session expired" || error === "Unauthorized") return "세션이 만료되었거나 접근 권한이 없습니다.";
		return error;
	}
	const extracted = extractErrorMessage(error);
	if (extracted) {
		if (extracted === "Session expired" || extracted === "Unauthorized") return "세션이 만료되었거나 접근 권한이 없습니다.";
		return extracted;
	}
	return "알 수 없는 오류가 발생했습니다.";
}
