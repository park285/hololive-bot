import type { SessionFact } from "@/session/controller";

const CHANNEL = "hololive-admin-session-facts";
const facts = new Set<SessionFact>(["login", "logout", "rotation", "local_cleanup"]);

/** SessionChannel은 token·쿠키·사용자 데이터 없이 확인할 사건 종류만 전송합니다. */
export class SessionChannel {
	private channel: BroadcastChannel | null = null;
	/** start는 이전 구독을 닫고 한 개의 사건 구독을 엽니다. */
	start(onFact: (fact: SessionFact) => void): void {
		this.stop();
		if (typeof BroadcastChannel === "undefined") return;
		this.channel = new BroadcastChannel(CHANNEL);
		this.channel.onmessage = (event: MessageEvent<unknown>) => {
			const value = event.data;
			if (typeof value !== "object" || value === null || Object.keys(value).length !== 1 || !("fact" in value)) return;
			for (const fact of facts) if (value.fact === fact) { onFact(fact); return; }
		};
	}
	/** send는 열린 channel에 사건 종류 하나만 전달합니다. */
	send = (fact: SessionFact): void => { this.channel?.postMessage({ fact }); };
	/** stop은 앱 수명이 끝날 때 구독 자원을 해제합니다. */
	stop(): void { this.channel?.close(); this.channel = null; }
}
