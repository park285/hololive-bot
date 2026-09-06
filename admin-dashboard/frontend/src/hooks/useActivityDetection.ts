import { useCallback, useEffect, useRef, useState } from "react";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

const CHANNEL_NAME = "admin_session";
const TAB_ID = crypto.randomUUID();
const THROTTLE_MS = 1000;
const BROADCAST_THROTTLE_MS = 5000;

type TabSyncMessage =
	| { type: "ACTIVITY"; timestamp: number }
	| { type: "SESSION_REFRESH"; timestamp: number; source?: string }
	| { type: "LOGOUT"; timestamp: number };

interface UseActivityDetectionOptions {
	enabled: boolean;
	idleTimeoutMs: number;
	onRemoteLogout?: () => void;
	onSessionRefresh?: () => void;
}

const parseTabSyncMessage = (value: unknown): TabSyncMessage | null => {
	if (typeof value !== "object" || value === null) {
		return null;
	}

	const candidate = value as Partial<TabSyncMessage>;
	if (candidate.type === "SESSION_REFRESH" && candidate.source === TAB_ID) return null;

	if (
		candidate.type === "ACTIVITY" &&
		typeof candidate.timestamp === "number"
	) {
		return { type: "ACTIVITY", timestamp: candidate.timestamp };
	}

	if (
		candidate.type === "SESSION_REFRESH" &&
		typeof candidate.timestamp === "number"
	) {
		return { type: "SESSION_REFRESH", timestamp: candidate.timestamp };
	}

	if (
		candidate.type === "LOGOUT" &&
		typeof candidate.timestamp === "number"
	) {
		return { type: "LOGOUT", timestamp: candidate.timestamp };
	}

	return null;
};

// 쿠키 적용 뒤 변경 사실만 알리고 각 탭은 서버에서 현재 CSRF를 다시 읽는다.
export const broadcastSessionRefresh = (): void => {
	if (typeof BroadcastChannel === "undefined") return;
	const channel = new BroadcastChannel(CHANNEL_NAME);
	channel.postMessage({ type: "SESSION_REFRESH", timestamp: Date.now(), source: TAB_ID } satisfies TabSyncMessage);
	channel.close();
};

export const broadcastSessionLogout = (): void => {
	if (typeof BroadcastChannel === "undefined") {
		return;
	}

	const channel = new BroadcastChannel(CHANNEL_NAME);
	channel.postMessage({
		type: "LOGOUT",
		timestamp: Date.now(),
	} satisfies TabSyncMessage);
	channel.close();
};

export function useActivityDetection({
	enabled,
	idleTimeoutMs,
	onRemoteLogout,
	onSessionRefresh,
}: UseActivityDetectionOptions) {
	const [isIdle, setIsIdle] = useState(false);
	const timeoutRef = useRef<number | null>(null);
	const channelRef = useRef<BroadcastChannel | null>(null);
	const lastActivityRef = useRef<number>(0);
	const lastBroadcastRef = useRef<number>(0);
	const markSessionActivity = useSessionWarningStore(
		(state) => state.markSessionActivity,
	);

	const clearIdleTimer = useCallback(() => {
		if (timeoutRef.current !== null) {
			window.clearTimeout(timeoutRef.current);
			timeoutRef.current = null;
		}
	}, []);

	const resetTimerInternal = useCallback(
		(nowMs = Date.now()) => {
			if (!enabled) {
				return;
			}

			clearIdleTimer();
			setIsIdle(false);
			markSessionActivity(nowMs);

			timeoutRef.current = window.setTimeout(() => {
				setIsIdle(true);
			}, idleTimeoutMs);
		},
		[clearIdleTimer, enabled, idleTimeoutMs, markSessionActivity],
	);

	const resetTimer = useCallback(() => {
		if (!enabled) {
			return;
		}

		const now = Date.now();

		if (now - lastActivityRef.current < THROTTLE_MS) {
			return;
		}

		lastActivityRef.current = now;
		resetTimerInternal(now);

		if (now - lastBroadcastRef.current < BROADCAST_THROTTLE_MS) {
			return;
		}

		channelRef.current?.postMessage({
			type: "ACTIVITY",
			timestamp: now,
		} satisfies TabSyncMessage);
		lastBroadcastRef.current = now;
	}, [enabled, resetTimerInternal]);

	useEffect(() => {
		if (!enabled || typeof BroadcastChannel === "undefined") {
			return;
		}

		const channel = new BroadcastChannel(CHANNEL_NAME);
		channelRef.current = channel;

		channel.onmessage = (event: MessageEvent<unknown>) => {
			const message = parseTabSyncMessage(event.data);
			if (!message) {
				return;
			}

			if (message.type === "ACTIVITY") {
				resetTimerInternal(Date.now());
				return;
			}
			if (message.type === "SESSION_REFRESH") {
				onSessionRefresh?.();
				return;
			}

			onRemoteLogout?.();
		};

		return () => {
			channel.close();
			if (channelRef.current === channel) {
				channelRef.current = null;
			}
		};
	}, [enabled, onRemoteLogout, onSessionRefresh, resetTimerInternal]);

	useEffect(() => {
		if (!enabled) {
			clearIdleTimer();
			setIsIdle(false);
			return;
		}

		const events = ["mousemove", "keydown", "click", "scroll", "touchstart"];

		events.forEach((event) => {
			document.addEventListener(event, resetTimer, { passive: true });
		});

		resetTimerInternal();

		return () => {
			events.forEach((event) => {
				document.removeEventListener(event, resetTimer);
			});
			clearIdleTimer();
		};
	}, [clearIdleTimer, enabled, resetTimer, resetTimerInternal]);

	return isIdle;
}
