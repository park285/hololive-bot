import { useEffect } from "react";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export function useSessionWarnings(isIdle: boolean) {
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const {
		policy,
		absoluteExpiresAt,
		lastActivityAtMs,
		openIdleWarning,
		closeIdleWarning,
		openAbsoluteWarning,
		closeAbsoluteWarning,
	} = useSessionWarningStore();

	useEffect(() => {
		if (!isAuthenticated || !policy) {
			closeIdleWarning();
			closeAbsoluteWarning();
			return;
		}

		const interval = setInterval(() => {
			const now = Date.now();

			// 1. Idle Warning Check
			const idleTimeMs = now - lastActivityAtMs;
			if (
				idleTimeMs >= policy.idle_warning_timeout_ms &&
				idleTimeMs < policy.idle_timeout_ms
			) {
				openIdleWarning();
			} else {
				// If we're past idle_timeout_ms, heartbeat will handle logout.
				// If we've had activity, lastActivityAtMs would have been updated and idleTimeMs would be small.
				closeIdleWarning();
			}

			// 2. Absolute Warning Check
			if (absoluteExpiresAt) {
				const absoluteExpiresAtMs = absoluteExpiresAt * 1000;
				const timeToAbsoluteExpiryMs = absoluteExpiresAtMs - now;

				if (
					timeToAbsoluteExpiryMs > 0 &&
					timeToAbsoluteExpiryMs <= policy.absolute_warning_window_ms
				) {
					openAbsoluteWarning();
				} else {
					closeAbsoluteWarning();
				}
			}
		}, 1000);

		return () => {
			clearInterval(interval);
		};
	}, [
		isAuthenticated,
		policy,
		absoluteExpiresAt,
		lastActivityAtMs,
		openIdleWarning,
		closeIdleWarning,
		openAbsoluteWarning,
		closeAbsoluteWarning,
	]);

	useEffect(() => {
		if (isIdle) {
			closeIdleWarning();
		}
	}, [isIdle, closeIdleWarning]);
}
