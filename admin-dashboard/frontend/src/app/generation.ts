import { GenerationError, RequestBlockedError } from "@/api/errors";

export type GenerationStatus = { phase: "checking" | "ready" | "unavailable" | "incompatible"; message: string };

/** GenerationGate는 metadata 확인 전과 확인된 불일치 이후의 새 요청을 차단합니다. */
export class GenerationGate {
	private current: GenerationStatus = { phase: "checking", message: "서버와 앱의 호환성을 확인 중입니다." };
	private listeners = new Set<() => void>();
	private readonly onBlocked: () => void;
	constructor(onBlocked: () => void) { this.onBlocked = onBlocked; }
	snapshot = (): GenerationStatus => this.current;
	subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
	private set(next: GenerationStatus): void { this.current = next; for (const listener of this.listeners) listener(); }
	ready(): void { if (this.current.phase !== "incompatible") this.set({ phase: "ready", message: "" }); }
	unavailable(message: string): void { if (this.current.phase !== "incompatible") this.set({ phase: "unavailable", message }); }
	checking(): void { if (this.current.phase !== "incompatible") this.set({ phase: "checking", message: "서버와 앱의 호환성을 확인 중입니다." }); }
	block = (error: GenerationError): void => {
		if (this.current.phase === "incompatible") return;
		this.set({ phase: "incompatible", message: error.message });
		this.onBlocked();
	};
	assertReady = (): void => {
		if (this.current.phase !== "ready") throw new RequestBlockedError("CLIENT_NOT_READY", this.current.message);
	};
}
