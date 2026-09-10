import { RequestBlockedError } from "@/api/errors";

const AUTH_COOKIE_LOCK = "hololive-admin-auth-cookies";

/** CookieLock은 공유 쿠키를 바꾸는 응답의 순서를 브라우저 탭 사이에서 보장합니다. */
export interface CookieLock {
	run<T>(kind: "read" | "action", work: () => Promise<T>, signal?: AbortSignal): Promise<T>;
}

/** browserCookieLock은 인증 동작을 대기열에 넣지 않으며 조회 대기만 제한 시간 안에서 허용합니다. */
export function browserCookieLock(timeoutMs: number): CookieLock {
	return {
		async run<T>(kind: "read" | "action", work: () => Promise<T>, signal?: AbortSignal): Promise<T> {
			const browser: { navigator?: { locks?: LockManager } } = globalThis;
			const locks = browser.navigator?.locks;
			if (!locks) throw new RequestBlockedError("LOCKS_UNAVAILABLE", "이 브라우저에서는 안전한 세션 동기화를 사용할 수 없습니다.");
			signal?.throwIfAborted();
			const options: LockOptions = kind === "action" ? { ifAvailable: true } : {
				signal: AbortSignal.any([AbortSignal.timeout(timeoutMs), ...(signal ? [signal] : [])]),
			};
			return locks.request(AUTH_COOKIE_LOCK, options, async lock => {
				if (lock === null) throw new RequestBlockedError("SESSION_BUSY", "다른 탭에서 세션을 확인 중입니다. 잠시 후 다시 시도해 주세요.");
				signal?.throwIfAborted();
				// 전송 뒤 화면 취소로 잠금을 먼저 해제하면 늦은 Set-Cookie가 새 로그인을 덮어씁니다.
				// work의 HTTP 제한 시간 안에서 응답 처리를 끝낸 다음에만 잠금을 해제합니다.
				return work();
			});
		},
	};
}
