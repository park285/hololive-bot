import { useCallback, useEffect, useRef } from "react";
import { authApi } from "@/api/core";
import { CONFIG } from "@/config";
import { queryClient } from "@/lib/queryClient";
import toast from "@/lib/toast-api";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export const useHeartbeat = (isIdle: boolean) => {
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const logout = useAuthStore((state) => state.logout);
	const setAbsoluteExpiresAt = useSessionWarningStore((state) => state.setAbsoluteExpiresAt);
	
	const intervalRef = useRef<number | null>(null);
	const failCountRef = useRef(0);
	const abortControllerRef = useRef<AbortController | null>(null);
	const isIdleRef = useRef(isIdle);

	useEffect(() => {
		isIdleRef.current = isIdle;
	}, [isIdle]);

	const sendHeartbeat = useCallback(
		async (idle: boolean) => {
			if (abortControllerRef.current) {
				abortControllerRef.current.abort();
			}

			const controller = new AbortController();
			abortControllerRef.current = controller;

			try {
				const response = await authApi.heartbeat(idle, controller.signal);

				if (response.idle_rejected) {
					console.warn("Session idle warning (grace period)");
					void authApi.logout().catch(() => undefined);
					logout();
					queryClient.clear();
					return;
				}

				if (response.absolute_expired) {
					console.warn("Session absolute timeout exceeded");
					toast.error(
						"보안을 위해 세션이 만료되었습니다. 다시 로그인해주세요.",
					);
					logout();
					queryClient.clear();
					return;
				}

				if (response.absolute_expires_at) {
					setAbsoluteExpiresAt(response.absolute_expires_at);
				}

				if (response.error) {
					if (
						response.error === "Session expired" ||
						response.error === "Unauthorized"
					) {
						logout();
						queryClient.clear();
						return;
					}
					throw new Error(response.error);
				}

				failCountRef.current = 0;
			} catch (e: unknown) {
				if (e instanceof Error && e.name === "AbortError") return;

				failCountRef.current += 1;
				console.warn(
					`Heartbeat failed (${String(failCountRef.current)}/${String(CONFIG.heartbeat.maxFailures)})`,
				);

				if (failCountRef.current >= CONFIG.heartbeat.maxFailures) {
					logout();
					queryClient.clear();
				}
			} finally {
				if (abortControllerRef.current === controller) {
					abortControllerRef.current = null;
				}
			}
		},
		[logout, setAbsoluteExpiresAt],
	);

	useEffect(() => {
		if (!isAuthenticated) return;

		const handleVisibilityChange = () => {
			if (document.visibilityState === "visible") {
				void sendHeartbeat(false);
			}
		};

		document.addEventListener("visibilitychange", handleVisibilityChange);
		return () => {
			document.removeEventListener("visibilitychange", handleVisibilityChange);
		};
	}, [isAuthenticated, sendHeartbeat]);

	useEffect(() => {
		if (!isAuthenticated) return;

		if (isIdle) {
			void sendHeartbeat(true);
		}
	}, [isAuthenticated, isIdle, sendHeartbeat]);

	useEffect(() => {
		if (!isAuthenticated) return;

		void sendHeartbeat(isIdleRef.current);

		intervalRef.current = window.setInterval(() => {
			void sendHeartbeat(isIdleRef.current);
		}, CONFIG.heartbeat.intervalMs);

		return () => {
			if (intervalRef.current !== null) {
				window.clearInterval(intervalRef.current);
			}
			if (abortControllerRef.current) {
				abortControllerRef.current.abort();
			}
			failCountRef.current = 0;
		};
	}, [isAuthenticated, sendHeartbeat]);
};
