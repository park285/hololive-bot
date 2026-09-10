import { AxiosError, AxiosHeaders, type AxiosInstance, type AxiosResponse } from "axios";
import type { FullRequestParams, HttpClient } from "@/api/generated/http-client";
import operationContracts from "@/api/generated/operation-contracts";
import { loadOperationValidators, type ValidationModule } from "@/api/generated/validation";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { ContractError, GenerationError, RequestBlockedError, SessionChangedError } from "@/api/errors";

/** Operation은 정본에서 생성된 method·접근·validator 선언입니다. */
export type Operation = (typeof operationContracts)[number];
const operations = new Map(operationContracts.map(operation => [operation.id, operation]));

/** TransportContext는 요청이 시작한 인증 세대와 메모리 CSRF를 전달합니다. */
export interface TransportContext {
	readonly authGeneration: number;
	readonly csrfToken: string | null;
}

/** TransportPolicy는 앱의 요청 허용·인증·세대 정리를 HTTP 경계에 주입합니다. */
export interface TransportPolicy {
	execute<T>(operation: Operation, work: (markDispatched: () => void) => Promise<AxiosResponse<T>>): Promise<AxiosResponse<T>>;
	context(): TransportContext;
	beforeRequest(operation: Operation): void;
	onUnauthorized(authGeneration: number): void;
	onGenerationError(error: GenerationError): void;
}

function validate(validators: ValidationModule, name: string, value: unknown): boolean {
	const validator: unknown = Reflect.get(validators, name);
	if (typeof validator !== "function") throw new ContractError("계약 검증기를 찾지 못했습니다.", false);
	return Reflect.apply(validator, undefined, [value]) === true;
}

async function prepareValidation(operation: Operation, timeout: number, signal: FullRequestParams["signal"]): Promise<ValidationModule> {
	if (signal?.aborted) throw new DOMException("요청 준비가 취소되었습니다.", "AbortError");
	let timer: ReturnType<typeof setTimeout> | undefined;
	let abort: (() => void) | undefined;
	try {
		return await Promise.race([
			loadOperationValidators(operation.id).catch(() => { throw new RequestBlockedError("VALIDATION_UNAVAILABLE", "요청 검증 코드를 불러오지 못했습니다. 새로고침해 주세요."); }),
			new Promise<never>((_, reject) => {
				timer = setTimeout(() => { reject(new RequestBlockedError("VALIDATION_UNAVAILABLE", "요청 검증 코드 준비 시간이 초과되었습니다. 요청을 보내지 않았습니다.")); }, timeout);
				abort = () => { reject(new DOMException("요청 준비가 취소되었습니다.", "AbortError")); };
				signal?.addEventListener?.("abort", abort, { once: true });
			}),
		]);
	} finally {
		clearTimeout(timer);
		if (abort) signal?.removeEventListener?.("abort", abort);
	}
}

function pathParameters(operation: Operation, actual: string): Record<string, string> {
	const expected = operation.path.split("/");
	const parts = actual.split("/");
	if (expected.length !== parts.length) throw new ContractError("요청 경로가 계약과 다릅니다.", false);
	const values: Record<string, string> = {};
	for (const [index, segment] of expected.entries()) {
		const part = parts[index];
		if (part === undefined) throw new ContractError("요청 경로가 불완전합니다.", false);
		if (segment.startsWith("{") && segment.endsWith("}")) {
			try { values[segment.slice(1, -1)] = decodeURIComponent(part); }
			catch { throw new ContractError("요청 경로를 해석하지 못했습니다.", false); }
		} else if (segment !== part) throw new ContractError("요청 경로가 계약과 다릅니다.", false);
	}
	return values;
}

function responseHeader(response: AxiosResponse<unknown>, name: string): unknown {
	return response.headers instanceof AxiosHeaders ? response.headers.get(name) : response.headers[name.toLowerCase()];
}

/** createSDKTransport는 생성 계약으로 입출력을 검증하고 주입한 HTTP client를 한 번 호출합니다. */
export function createSDKTransport(client: AxiosInstance, policy: TransportPolicy): HttpClient {
	return {
		async request<T>(params: FullRequestParams): Promise<AxiosResponse<T>> {
			const { timeout } = client.defaults;
			if (typeof timeout !== "number" || !Number.isFinite(timeout) || timeout <= 0) throw new Error("Administrator transport requires a finite positive timeout");
			const operation = operations.get(params.operationId);
			if (!operation || operation.method !== params.method) throw new ContractError("등록되지 않은 요청입니다.", false);
			return policy.execute<T>(operation, async markDispatched => {
				const context = policy.context();
				const deadline = performance.now() + timeout;
				policy.beforeRequest(operation);
				// C07: 입력과 모든 응답 검증기를 전송 전에 준비하며 HTTP와 같은 시간 예산을 씁니다.
				const validators = await prepareValidation(operation, timeout, params.signal);
				const current = policy.context();
				if (operation.access !== "public_metadata" && current.authGeneration !== context.authGeneration) throw new SessionChangedError();
				if (params.signal?.aborted) throw new DOMException("요청 준비가 취소되었습니다.", "AbortError");
				for (const name of [operation.request, ...Object.values(operation.responses).map(response => response.validator)]) {
					if (typeof validators[name] !== "function") throw new ContractError("계약 검증기를 찾지 못했습니다.", false);
				}
				const headers: Record<string, string> = {};
				// ASVS 2.3.1/2.3.2: 브라우저의 HTTP 재전송도 BFF가 같은 변경 ID로 거부합니다(C02).
				if (operation.mutation) headers["x-admin-mutation-id"] = crypto.randomUUID();
				if (operation.access !== "public_metadata") headers["x-admin-client-generation"] = CLIENT_GENERATION;
				if (operation.access === "session_csrf" || operation.access === "session_csrf_audit") {
					const token = current.csrfToken;
					if (token !== null) headers["x-csrf-token"] = token;
				}
				const input = { path: pathParameters(operation, params.path), query: params.query ?? {}, headers, body: params.body };
				if (!validate(validators, operation.request, input)) throw new ContractError("요청 입력이 관리자 계약과 다릅니다.", false);
				policy.beforeRequest(operation);
				const baseURL = client.defaults.baseURL?.replace(/\/admin\/api\/?$/, "") ?? "";
				const remaining = Math.floor(deadline - performance.now());
				if (remaining <= 0) throw new RequestBlockedError("VALIDATION_UNAVAILABLE", "요청 준비 시간이 초과되어 요청을 보내지 않았습니다.");
				markDispatched();
				const response = await client.request<unknown>({
					baseURL, url: params.path, method: params.method, params: params.query, data: params.body,
					signal: params.signal, headers, timeout: remaining, validateStatus: () => true,
			});
			const generation = responseHeader(response, "X-Admin-Server-Generation");
			if (generation !== CLIENT_GENERATION) {
				const error = new GenerationError(typeof generation === "string" && generation !== "" ? "different" : "missing");
				policy.onGenerationError(error);
				throw error;
			}
			const declared: unknown = Reflect.get(operation.responses, String(response.status));
			if (typeof declared !== "object" || declared === null || !("validator" in declared) || typeof declared.validator !== "string") {
				throw new ContractError("선언되지 않은 응답 상태입니다.", true);
			}
			const contentType = responseHeader(response, "Content-Type");
			if (typeof contentType !== "string" || contentType.split(";")[0]?.trim().toLowerCase() !== "application/json" ||
				!validate(validators, declared.validator, { headers: { "x-admin-server-generation": generation }, body: response.data })) {
				throw new ContractError("응답 형식을 확인하지 못했습니다. 작업 결과는 다시 확인해야 합니다.", true);
			}
			if (response.status >= 400) {
				if (response.status === 401 && operation.access !== "public_login") policy.onUnauthorized(context.authGeneration);
				throw new AxiosError("관리자 요청이 거부되었습니다.", "ERR_BAD_RESPONSE", response.config, response.request, response);
			}
			// T는 같은 정본에서 생성한 operation 반환 타입이며, 위 validator가 unknown body를 검사했다.
			return { ...response, data: response.data as T };
			});
		},
	};
}
