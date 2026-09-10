import { isAxiosError } from "axios";
import type { Admin } from "@/api/generated/Admin";
import type { HeartbeatResponse, SessionStatusResponse } from "@/api/generated/data-contracts";
import { RequestBlockedError, SessionChangedError } from "@/api/errors";
import type { CookieLock } from "@/session/cookie-lock";
import type { SessionState } from "@/session/state";

export type SessionFact = "login" | "logout" | "rotation" | "local_cleanup";

export interface SessionEvents {
	boundary(): void;
	changed(fact: SessionFact): void;
}

export type LogoutResult = { revocation: "confirmed" | "unknown"; clientCleanup: "completed" };

/** SessionController는 쿠키 쓰기·세션 세대·CSRF 상태를 하나의 인증 경계에서 처리합니다. */
export class SessionController {
	private readonly sdk: Admin;
	readonly state: SessionState;
	private readonly cookies: CookieLock;
	private readonly events: SessionEvents;
	constructor(sdk: Admin, state: SessionState, cookies: CookieLock, events: SessionEvents) {
		this.sdk = sdk; this.state = state; this.cookies = cookies; this.events = events;
	}

	private boundary(): number {
		const generation = this.state.boundary();
		this.events.boundary();
		return generation;
	}

	/** clearLocal은 민감한 로컬 상태만 정리하며 서버 폐기를 주장하지 않습니다. */
	clearLocal = (broadcast = false): void => {
		this.boundary();
		this.state.signedOut();
		if (broadcast) this.events.changed("local_cleanup");
	};

	/** unauthorized는 현재 인증 요청의 확인된 401만 적용합니다. */
	unauthorized = (generation: number): void => {
		if (generation === this.state.snapshot().authGeneration) this.clearLocal();
	};

	private async readCurrent(generation: number, signal?: AbortSignal): Promise<SessionStatusResponse> {
		this.state.assertCurrent(generation);
		const expected = this.state.snapshot();
		const { data } = await this.sdk.handleSessionStatus();
		signal?.throwIfAborted();
		this.state.acceptStatus(data, expected);
		return data;
	}

	/** refresh는 공유 쿠키로 세션을 확인하며 취소된 호출의 UI 상태를 적용하지 않습니다. */
	refresh = async (signal?: AbortSignal): Promise<SessionStatusResponse> => {
		const generation = this.state.snapshot().authGeneration;
		return this.cookies.run("read", async () => {
			const data = await this.readCurrent(generation, signal);
			signal?.throwIfAborted();
			return data;
		}, signal);
	};

	/** login은 쿠키 잠금 안에서 한 번 로그인하고 현재 세션을 확인합니다. */
	login = async (username: string, password: string): Promise<SessionStatusResponse> => {
		const data = await this.cookies.run("action", async () => {
			const generation = this.boundary();
			const expected = this.state.snapshot();
			try {
				const { data: login } = await this.sdk.handleLogin({ username, password });
				this.state.acceptCSRF(login.csrf_token, expected);
				return await this.readCurrent(generation);
			} catch (error) {
				if (generation === this.state.snapshot().authGeneration) this.state.signedOut();
				throw error;
			}
		});
		this.events.changed("login");
		return data;
	};

	/** logout은 서버 폐기 확인과 로컬 정리를 구분하며 POST를 재시도하지 않습니다. */
	logout = async (): Promise<LogoutResult> => this.cookies.run("action", async () => {
		const generation = this.state.snapshot().authGeneration;
		let revocation: LogoutResult["revocation"] = "unknown";
		try {
			await this.sdk.handleLogout();
			revocation = "confirmed";
		} catch (error) {
			if (error instanceof RequestBlockedError) throw error;
		} finally {
			if (generation === this.state.snapshot().authGeneration) {
				this.clearLocal();
			}
			this.events.changed("logout");
		}
		return { revocation, clientCleanup: "completed" };
	});

	/** heartbeat는 한 번 갱신하고 회전했을 때 현재 쿠키의 CSRF를 조회합니다. */
	heartbeat = async (idle = false, signal?: AbortSignal): Promise<HeartbeatResponse> => {
		const generation = this.state.snapshot().authGeneration;
		if (this.state.snapshot().csrfToken === null) throw new SessionChangedError();
		const data = await this.cookies.run("action", async () => {
			this.state.assertCurrent(generation);
			const { data: heartbeat } = await this.sdk.handleHeartbeat({ idle });
			this.state.assertCurrent(generation);
			if ("rotated" in heartbeat) {
				this.state.clearCSRF();
				await this.readCurrent(generation);
			}
			signal?.throwIfAborted();
			return heartbeat;
		}, signal);
		if ("rotated" in data) this.events.changed("rotation");
		if (data.status === "ok") this.state.acceptDeadline(data.absolute_expires_at, generation);
		return data;
	};

	/** remoteChanged는 탭 사건을 인증 증거로 쓰지 않고 서버를 다시 조회합니다. */
	remoteChanged = async (fact: SessionFact): Promise<void> => {
		if (fact === "rotation") this.state.clearCSRF();
		else this.boundary();
		try { await this.refresh(); }
		catch (error) {
			if (isAxiosError(error) && error.response?.status === 401 || error instanceof SessionChangedError) return;
			throw error;
		}
	};
}
