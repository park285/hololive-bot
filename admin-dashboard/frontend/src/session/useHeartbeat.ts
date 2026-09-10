import { RequestBlockedError } from "@/api/errors";
import { isAxiosError } from "axios";
import { useCallback, useEffect, useRef } from "react";
import { session } from "@/app/bootstrap";
import { CONFIG } from "@/config";
import toast from "@/lib/toast-api";
import { useSessionSnapshot } from "@/session/useSession";



const isCanceledRequest = (error: unknown): boolean => {
	if (error instanceof Error) {
		if (error.name === "AbortError" || error.name === "CanceledError") {
			return true;
		}
	}

	return isAxiosError(error) && error.code === "ERR_CANCELED";
};

export const useHeartbeat = (isIdle: boolean) => {
	const auth = useSessionSnapshot();
	const isAuthenticated = auth.phase === "authenticated";
	const {policy} = auth;

	const failCountRef = useRef(0);
	const abortControllerRef = useRef<AbortController | null>(null);
	const inFlightRef = useRef(false);
	const isIdleRef = useRef(isIdle);

	const intervalMs = policy?.heartbeat_interval_ms;

	useEffect(() => {
		isIdleRef.current = isIdle;
	}, [isIdle]);

	const expireSession = useCallback((message: string) => {
		void session.logout().then(result => {
			if (result.revocation === "unknown") toast.error("서버 세션 폐기를 확인하지 못했습니다.");
		}).catch((error: unknown) => {
			toast.error(error instanceof Error ? error.message : "서버 세션 폐기를 확인하지 못했습니다.");
		});
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
				const response = await session.heartbeat(idle, controller.signal);

				if (response.status === "idle") {
					expireSession("유휴 시간이 초과되어 로그아웃되었습니다.");
					return;
				}

				failCountRef.current = 0;
			} catch (error: unknown) {
				if (isCanceledRequest(error) || error instanceof RequestBlockedError) {
					return;
				}
				if (isAxiosError(error) && error.response?.status === 401) return;
				if (isAxiosError(error) && error.response?.status === 403) {
					// 다른 탭이 쿠키를 회전한 뒤의 CSRF 거절은 세션 만료의 증거가 아니다.
					// POST는 재전송하지 않고 권위 있는 GET으로 다음 요청의 상태만 복원한다.
					try {
						await session.refresh(controller.signal);
						failCountRef.current = 0;
						return;
					} catch (refreshError: unknown) {
						if (isCanceledRequest(refreshError)) return;
					}
				}

				failCountRef.current += 1;
				console.warn(
					`Heartbeat failed (${String(failCountRef.current)}/${String(CONFIG.heartbeat.maxFailures)})`,
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
		[expireSession, isAuthenticated],
	);

	useEffect(() => {
		if (!isAuthenticated) return;

		const handleVisibilityChange = () => {
			if (document.visibilityState === "visible") {
				void session.refresh().then(() => sendHeartbeat(false)).catch((error: unknown) => {
					if (!isCanceledRequest(error)) console.warn("세션 재개 확인에 실패했습니다.");
				});
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

		if (intervalMs === undefined) return;
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
