import { SessionChangedError } from "@/api/errors";
import type { SessionPolicyResponse, SessionStatusResponse } from "@/api/generated/data-contracts";

interface SessionContext {
	readonly authGeneration: number;
	readonly csrfVersion: number;
	readonly csrfToken: string | null;
}

export type SessionSnapshot = SessionContext & (
	{ readonly phase: "pending"; readonly policy: null; readonly absoluteExpiresAt: null }
	| { readonly phase: "signed_out"; readonly policy: null; readonly absoluteExpiresAt: null }
	| { readonly phase: "authenticated"; readonly policy: SessionPolicyResponse; readonly absoluteExpiresAt: number }
);

/** SessionState는 인증·정책·만료와 CSRF의 유일한 메모리 상태이며 세대가 다른 응답을 거부합니다. */
export class SessionState {
	private current: SessionSnapshot = { authGeneration: 0, csrfVersion: 0, csrfToken: null, phase: "pending", policy: null, absoluteExpiresAt: null };
	private listeners = new Set<() => void>();
	snapshot = (): SessionSnapshot => this.current;
	subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
	private publish(next: SessionSnapshot): void { this.current = next; for (const listener of this.listeners) listener(); }

	/** pending은 metadata/세션 확인 동안 보호된 화면을 비활성화합니다. */
	pending(): void { this.publish({ ...this.current, phase: "pending", policy: null, absoluteExpiresAt: null }); }

	/** boundary는 인증 경계를 바꾸고 이전 CSRF·정책을 폐기합니다. */
	boundary(): number {
		this.publish({ authGeneration: this.current.authGeneration + 1, csrfVersion: this.current.csrfVersion + 1, csrfToken: null, phase: "pending", policy: null, absoluteExpiresAt: null });
		return this.current.authGeneration;
	}

	/** signedOut은 불완전한 로그인 뒤 남은 CSRF도 제거하며 서버 폐기를 주장하지 않습니다. */
	signedOut(): void { this.publish({ ...this.current, csrfVersion: this.current.csrfVersion + 1, csrfToken: null, phase: "signed_out", policy: null, absoluteExpiresAt: null }); }

	/** assertCurrent는 이전 인증에 속한 응답의 적용을 거부합니다. */
	assertCurrent(generation: number): void { if (generation !== this.current.authGeneration) throw new SessionChangedError(); }

	/** clearCSRF는 인증 세대를 유지하며 권위 있는 재조회를 요구합니다. */
	clearCSRF(): void { this.publish({ ...this.current, csrfVersion: this.current.csrfVersion + 1, csrfToken: null }); }

	private verifiedCSRF(token: string, expected: SessionSnapshot): SessionContext {
		this.assertCurrent(expected.authGeneration);
		if (expected.csrfVersion !== this.current.csrfVersion) throw new SessionChangedError();
		return { authGeneration: this.current.authGeneration, csrfVersion: this.current.csrfVersion + Number(token !== this.current.csrfToken), csrfToken: token };
	}

	/** acceptCSRF는 두 세대가 모두 일치할 때 검증된 토큰만 저장합니다. */
	acceptCSRF(token: string, expected: SessionSnapshot): void { this.publish({ ...this.current, ...this.verifiedCSRF(token, expected) }); }

	/** acceptStatus는 검증한 현재 세션 응답을 인증·정책·CSRF 상태에 함께 적용합니다. */
	acceptStatus(status: SessionStatusResponse, expected: SessionSnapshot): void {
		this.publish({ ...this.verifiedCSRF(status.csrf_token, expected), phase: "authenticated", policy: status.session_policy, absoluteExpiresAt: status.absolute_expires_at });
	}

	/** acceptDeadline은 현재 인증의 heartbeat가 확인한 절대 만료만 갱신합니다. */
	acceptDeadline(absoluteExpiresAt: number, generation: number): void {
		this.assertCurrent(generation);
		if (this.current.phase === "authenticated") this.publish({ ...this.current, absoluteExpiresAt });
	}
}
