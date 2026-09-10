import { onlineManager } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";

const subscribe = (changed: () => void) => onlineManager.subscribe(changed);
const snapshot = () => onlineManager.isOnline();

/** useOnline은 QueryClient와 같은 연결 상태를 사용하며 앱 시작 시 navigator 상태로 초기화합니다. */
export function useOnline(): boolean { return useSyncExternalStore(subscribe, snapshot); }
