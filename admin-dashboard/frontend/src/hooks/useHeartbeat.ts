import { isAxiosError } from "axios";
import { useCallback, useEffect, useRef } from "react";
import { authApi } from "@/api/core";
import { CONFIG } from "@/config";
import { clearClientSession } from "@/lib/sessionLifecycle";
import toast from "@/lib/toast-api";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

const SESSION_EXPIRED_ERRORS = new Set(["Session expired", "Unauthorized"]);

const isCanceledRequest = (error: unknown): boolean => {
	if (error instanceof Error) {
		if (error.name === "AbortError" || error.name === "CanceledError") {
			return true;
		}
	}

	return isAxiosError(error) && error.code === "ERR_CANCELED";
};

export const useHeartbeat = (isIdle: boolean) => {
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const policy = useSessionWarningStore((state) => state.policy);
	const setAbsoluteExpiresAt = useSessionWarningStore(
		(state) => state.setAbsoluteExpiresAt,
	);

	const failCountRef = useRef(0);
	const abortControllerRef = useRef<AbortController | null>(null);
	const inFlightRef = useRef(false);
	const isIdleRef = useRef(isIdle);

	const intervalMs = policy?.heartbeat_interval_ms ?? CONFIG.heartbeat.intervalMs;

	useEffect(() => {
		isIdleRef.current = isIdle;
	}, [isIdle]);

	const expireSession = useCallback((message: string) => {
		void authApi.logout().catch(() => undefined);
		clearClientSession(true);
		toast.error(message);
	}, []);

	const sendHeartbeat = useCallback(
		async (idle: boolean) => {
			if (!isAuthenticated || inFlightRef.current) {
				return;
			}

			const controller = new AbortController();
			abortControllerRef.current = controller;
			inFlightRef.current = true;

			try {
				const response = await authApi.heartbeat(idle, controller.signal);

				if (response.absolute_expires_at !== undefined) {
					setAbsoluteExpiresAt(response.absolute_expires_at);
				}

				if (response.idle_rejected) {
					expireSession(
						response.error ?? "유휴 시간이 초과되어 로그아웃되었습니다.",
					);
					return;
				}

				if (response.absolute_expired) {
					expireSession(
						response.error ??
							"보안을 위해 세션이 만료되었습니다. 다시 로그인해주세요.",
					);
					return;
				}

				if (response.error) {
					if (SESSION_EXPIRED_ERRORS.has(response.error)) {
						expireSession("세션이 만료되었거나 접근 권한이 없습니다.");
						return;
					}

					throw new Error(response.error);
				}

				failCountRef.current = 0;
			} catch (error: unknown) {
				if (isCanceledRequest(error)) {
					return;
				}

				failCountRef.current += 1;
				console.warn(
					`Heartbeat failed (${String(failCountRef.current)}/${String(CONFIG.heartbeat.maxFailures)})`,
					error,
				);

				if (failCountRef.current >= CONFIG.heartbeat.maxFailures) {
					expireSession(
						"서버와 세션을 확인하지 못해 안전을 위해 로그아웃했습니다.",
					);
				}
			} finally {
				if (abortControllerRef.current === controller) {
					abortControllerRef.current = null;
					inFlightRef.current = false;
				}
			}
		},
		[expireSession, isAuthenticated, setAbsoluteExpiresAt],
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
		if (!isAuthenticated) {
			failCountRef.current = 0;
			return;
		}

		void sendHeartbeat(isIdleRef.current);

		const intervalId = window.setInterval(() => {
			void sendHeartbeat(isIdleRef.current);
		}, intervalMs);

		return () => {
			window.clearInterval(intervalId);
			abortControllerRef.current?.abort();
			abortControllerRef.current = null;
			inFlightRef.current = false;
			failCountRef.current = 0;
		};
	}, [intervalMs, isAuthenticated, sendHeartbeat]);
};
