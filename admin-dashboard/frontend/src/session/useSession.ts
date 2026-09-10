import { useSyncExternalStore } from "react";
import { session } from "@/app/bootstrap";

/** useSessionSnapshot은 별도 인증 store 없이 controller의 현재 상태를 구독합니다. */
export function useSessionSnapshot() {
	return useSyncExternalStore(session.state.subscribe, session.state.snapshot);
}
