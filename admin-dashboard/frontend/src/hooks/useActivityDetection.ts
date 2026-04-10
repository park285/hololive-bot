import { useCallback, useEffect, useRef, useState } from "react";

const CHANNEL_NAME = "admin_session";
const THROTTLE_MS = 1000; // Local throttle: 1s
const BROADCAST_THROTTLE_MS = 5000; // Broadcast throttle: 5s

interface TabSyncMessage {
	type: "ACTIVITY" | "LOGOUT";
	timestamp: number;
}

export function useActivityDetection(idleTimeoutMs: number) {
	const [isIdle, setIsIdle] = useState(false);
	const timeoutRef = useRef<number | null>(null);
	const channelRef = useRef<BroadcastChannel | null>(null);
	const lastActivityRef = useRef<number>(0);
	const lastBroadcastRef = useRef<number>(0);

	const resetTimerInternal = useCallback(() => {
		setIsIdle(false);

		if (timeoutRef.current) {
			window.clearTimeout(timeoutRef.current);
		}

		timeoutRef.current = window.setTimeout(() => {
			setIsIdle(true);
		}, idleTimeoutMs);
	}, [idleTimeoutMs]);

	const resetTimer = useCallback(() => {
		const now = Date.now();

		if (now - lastActivityRef.current < THROTTLE_MS) {
			return;
		}
		lastActivityRef.current = now;
		resetTimerInternal();

		if (now - lastBroadcastRef.current < BROADCAST_THROTTLE_MS) {
			return;
		}

		if (channelRef.current) {
			const message: TabSyncMessage = {
				type: "ACTIVITY",
				timestamp: now,
			};
			channelRef.current.postMessage(message);
			lastBroadcastRef.current = now;
		}
	}, [resetTimerInternal]);

	useEffect(() => {
		if (typeof BroadcastChannel === "undefined") {
			return;
		}

		channelRef.current = new BroadcastChannel(CHANNEL_NAME);

		channelRef.current.onmessage = (event: MessageEvent<TabSyncMessage>) => {
			if (event.data.type === "ACTIVITY") {
				resetTimerInternal();
			}
		};

		return () => {
			channelRef.current?.close();
			channelRef.current = null;
		};
	}, [resetTimerInternal]);

	useEffect(() => {
		const events = ["mousemove", "keydown", "click", "scroll", "touchstart"];

		events.forEach((event) => {
			document.addEventListener(event, resetTimer, { passive: true });
		});

		resetTimerInternal();

		return () => {
			events.forEach((event) => {
				document.removeEventListener(event, resetTimer);
			});
			if (timeoutRef.current) {
				window.clearTimeout(timeoutRef.current);
			}
		};
	}, [resetTimer, resetTimerInternal]);

	return isIdle;
}
