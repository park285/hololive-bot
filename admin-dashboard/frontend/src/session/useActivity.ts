import { useCallback, useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { warningState } from "@/session/warnings";

const CHANNEL_NAME = "hololive-admin-activity";
const THROTTLE_MS = 1000;
const BROADCAST_THROTTLE_MS = 5000;

interface UseActivityDetectionOptions { enabled: boolean; idleTimeoutMs: number; }

export function useActivityDetection({
	enabled,
	idleTimeoutMs,
}: UseActivityDetectionOptions) {
	const [isIdle, setIsIdle] = useState(false);
	const timeoutRef = useRef<number | null>(null);
	const channelRef = useRef<BroadcastChannel | null>(null);
	const lastActivityRef = useRef<number>(0);
	const lastBroadcastRef = useRef<number>(0);
	const markSessionActivity = useStore(warningState,
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
		});
		lastBroadcastRef.current = now;
	}, [enabled, resetTimerInternal]);

	useEffect(() => {
		if (!enabled || typeof BroadcastChannel === "undefined") {
			return;
		}

		const channel = new BroadcastChannel(CHANNEL_NAME);
		channelRef.current = channel;

		channel.onmessage = (event: MessageEvent<unknown>) => {
			const value = event.data;
			if (typeof value === "object" && value !== null && "type" in value && value.type === "ACTIVITY") resetTimerInternal(Date.now());
		};

		return () => {
			channel.close();
			if (channelRef.current === channel) {
				channelRef.current = null;
			}
		};
	}, [enabled, resetTimerInternal]);

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
