import { session } from "@/app/bootstrap";
import { useEffect, useRef } from "react";
import toast from "@/lib/toast-api";
import { useSessionSnapshot } from "@/session/useSession";
import { useStore } from "zustand";
import { warningState } from "@/session/warnings";

export function useSessionWarnings(isIdle: boolean) {
	const auth = useSessionSnapshot();
	const isAuthenticated = auth.phase === "authenticated";
	const {
		absoluteWarningDismissedForExpiresAt,
		lastActivityAtMs,
		openIdleWarning,
		closeIdleWarning,
		openAbsoluteWarning,
		closeAbsoluteWarning,
	} = useStore(warningState);
	const { policy, absoluteExpiresAt } = auth;
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

			const absoluteExpiresAtMs = absoluteExpiresAt * 1000;
			const timeToAbsoluteExpiryMs = absoluteExpiresAtMs - now;

			if (timeToAbsoluteExpiryMs <= 0) {
				closeAbsoluteWarning();
				if (expiredExpiresAtRef.current !== absoluteExpiresAt) {
					expiredExpiresAtRef.current = absoluteExpiresAt;
					toast.error(
						"보안을 위해 세션이 만료되었습니다. 다시 로그인해주세요.",
					);
					session.clearLocal(true);
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
