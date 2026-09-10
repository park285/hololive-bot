/** ContractError는 전송 전 입력 실패와 전송 후 미확정 응답을 구분합니다. */
export class ContractError extends Error {
	readonly dispatched: boolean;
	constructor(message: string, dispatched: boolean) {
		super(message);
		this.dispatched = dispatched;
		this.name = "ContractError";
	}
}

/** GenerationError는 응답으로 확인한 세대 차이와 세대 증거 부재를 구분합니다. */
export class GenerationError extends ContractError {
	readonly reason: "different" | "missing";
	constructor(reason: "different" | "missing") {
		super(reason === "different" ? "관리자 앱이 변경되었습니다. 새로고침해 주세요." : "서버와 브라우저의 호환성을 확인하지 못했습니다. 새로고침해 주세요.", true);
		this.reason = reason;
		this.name = "GenerationError";
	}
}

/** RequestBlockedError는 전송하지 않은 요청이며 자동 대기·재실행을 뜻하지 않습니다. */
export class RequestBlockedError extends ContractError {
	readonly code: "CLIENT_NOT_READY" | "SESSION_BUSY" | "LOCKS_UNAVAILABLE" | "OFFLINE" | "BUSY" | "VALIDATION_UNAVAILABLE";
	constructor(code: RequestBlockedError["code"], message: string) {
		super(message, false);
		this.code = code;
		this.name = "RequestBlockedError";
	}
}

/** SessionChangedError는 이전 세션 응답을 현재 UI에 적용하지 않게 합니다. */
export class SessionChangedError extends Error {
	constructor() { super("확인 중 세션이 변경되었습니다."); this.name = "AbortError"; }
}
