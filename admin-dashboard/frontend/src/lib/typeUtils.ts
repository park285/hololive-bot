export function extractErrorMessage(data: unknown): string | undefined {
	if (typeof data === "object" && data !== null && "message" in data) {
		const { message } = data as { message: unknown };
		return typeof message === "string" ? message : String(message);
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
	if (error instanceof Error) {
		return error.message;
	}
	if (typeof error === "string") {
		return error;
	}
	const extracted = extractErrorMessage(error);
	if (extracted) {
		return extracted;
	}
	return "알 수 없는 오류가 발생했습니다.";
}
