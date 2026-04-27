import { useEffect, useRef } from "react";
import { clearClientSession } from "@/lib/sessionLifecycle";
import toast from "@/lib/toast-api";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export function useSessionWarnings(isIdle: boolean) {
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const {
		policy,
		absoluteExpiresAt,
		absoluteWarningDismissedForExpiresAt,
		lastActivityAtMs,
		openIdleWarning,
		closeIdleWarning,
		openAbsoluteWarning,
		closeAbsoluteWarning,
	} = useSessionWarningStore();
	const expiredExpiresAtRef = useRef<number | null>(null);

	useEffect(() => {
		if (absoluteExpiresAt !== expiredExpiresAtRef.current) {
			expiredExpiresAtRef.current = null;
		}
	}, [absoluteExpiresAt]);

	useEffect(() => {
		if (!isAuthenticated || !policy) {
			closeIdleWarning();
			closeAbsoluteWarning();
			return;
		}

		const evaluateWarnings = () => {
			const now = Date.now();

			const idleTimeMs = now - lastActivityAtMs;
			const shouldShowIdleWarning =
				!isIdle &&
				idleTimeMs >= policy.idle_warning_timeout_ms &&
				idleTimeMs < policy.idle_timeout_ms;

			if (shouldShowIdleWarning) {
				openIdleWarning();
			} else {
				closeIdleWarning();
			}

			if (absoluteExpiresAt === null) {
				closeAbsoluteWarning();
				return;
			}

			const absoluteExpiresAtMs = absoluteExpiresAt * 1000;
			const timeToAbsoluteExpiryMs = absoluteExpiresAtMs - now;

			if (timeToAbsoluteExpiryMs <= 0) {
				closeAbsoluteWarning();
				if (expiredExpiresAtRef.current !== absoluteExpiresAt) {
					expiredExpiresAtRef.current = absoluteExpiresAt;
					toast.error(
						"보안을 위해 세션이 만료되었습니다. 다시 로그인해주세요.",
					);
					clearClientSession(true);
				}
				return;
			}

			const shouldShowAbsoluteWarning =
				timeToAbsoluteExpiryMs <= policy.absolute_warning_window_ms &&
				absoluteWarningDismissedForExpiresAt !== absoluteExpiresAt;

			if (shouldShowAbsoluteWarning) {
				openAbsoluteWarning();
			} else {
				closeAbsoluteWarning();
			}
		};

		evaluateWarnings();
		const interval = window.setInterval(evaluateWarnings, 1000);

		return () => {
			window.clearInterval(interval);
		};
	}, [
		absoluteExpiresAt,
		absoluteWarningDismissedForExpiresAt,
		closeAbsoluteWarning,
		closeIdleWarning,
		isAuthenticated,
		isIdle,
		lastActivityAtMs,
		openAbsoluteWarning,
		openIdleWarning,
		policy,
	]);
}
