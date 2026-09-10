import { isAxiosError } from "axios";
import { onlineManager } from "@tanstack/react-query";
import { createAdminClient } from "@/api/client";
import { GenerationError, RequestBlockedError } from "@/api/errors";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { GenerationGate } from "@/app/generation";
import { CONFIG } from "@/config/constants";
import { queryClient } from "@/queries/client";
import { browserCookieLock } from "@/session/cookie-lock";
import { SessionChannel } from "@/session/channel";
import { SessionController } from "@/session/controller";
import { SessionState } from "@/session/state";
import { Operations } from "@/operations/controller";
import { warningState } from "@/session/warnings";

function clearSensitiveState(): void {
	operations.hideSensitive();
	void queryClient.cancelQueries();
	queryClient.clear();
	warningState.getState().resetSessionWarnings();
}

const state = new SessionState();
export const operations = new Operations(() => state.snapshot().authGeneration, () => navigator.onLine && onlineManager.isOnline());
const channel = new SessionChannel();
export const generation = new GenerationGate(() => { session.clearLocal(); });

const client = createAdminClient(CONFIG.api.baseUrl, CONFIG.api.timeoutMs, {
	execute(operation, work) { return operation.mutation ? operations.execute(operation, work) : work(() => {}); },
	context: state.snapshot,
	beforeRequest(operation) { if (operation.access !== "public_metadata") generation.assertReady(); },
	onUnauthorized(epoch) { session.unauthorized(epoch); },
	onGenerationError: generation.block,
});

export const adminClient = client.sdk;
export const httpClient = client.http;
export const session = new SessionController(adminClient, state, browserCookieLock(CONFIG.api.timeoutMs), {
	boundary: clearSensitiveState,
	changed: channel.send,
});

/** verifyMetadata는 최초 실행과 매 WS 연결 전에 생성 SDK로 호환성을 검사합니다. */
export async function verifyMetadata(signal?: AbortSignal): Promise<void> {
	if (generation.snapshot().phase === "incompatible") generation.assertReady();
	const { data, headers } = await adminClient.getAdminMetadata({ signal });
	if (data.clientGeneration !== CLIENT_GENERATION) {
		const error = new GenerationError("different");
		generation.block(error);
		throw error;
	}
	const cacheControl: unknown = headers["cache-control"];
	if (typeof cacheControl !== "string" || !cacheControl.split(",").some(part => part.trim().toLowerCase() === "no-store")) {
		throw new RequestBlockedError("CLIENT_NOT_READY", "서버 호환성 정보의 현재성을 확인하지 못했습니다.");
	}
	generation.ready();
}

/** bootstrapApplication은 metadata·현재 세션 확인을 앱 시작 경계에서 한 번 수행합니다. */
export async function bootstrapApplication(): Promise<void> {
	generation.checking();
	state.pending();
	onlineManager.setOnline(navigator.onLine);
	channel.start(fact => {
		if (generation.snapshot().phase !== "ready") return;
		void session.remoteChanged(fact).catch(() => { generation.unavailable("다른 탭의 세션 변경을 확인하지 못했습니다. 다시 확인해 주세요."); });
	});
	try {
		await verifyMetadata();
		try { await session.refresh(); }
		catch (error) { if (!isAxiosError(error) || error.response?.status !== 401) throw error; }
	} catch (error) {
		generation.unavailable(error instanceof Error ? error.message : "관리자 앱을 준비하지 못했습니다.");
	}
}

if (import.meta.hot) import.meta.hot.dispose(() => { channel.stop(); });
