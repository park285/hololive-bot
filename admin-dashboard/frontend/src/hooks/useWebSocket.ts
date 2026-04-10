import { useCallback, useEffect, useRef, useState } from "react";
import { CONFIG } from "@/config/constants";

interface WebSocketOptions<T> {
	parseMessage?: (data: unknown) => T | null;
	onMessage?: (data: T) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (event: Event) => void;
	autoConnect?: boolean;
	reconnectAttempts?: number;
	reconnectInterval?: number;
	/** Keep-alive ping 활성화 여부 (기본: true) */
	enablePing?: boolean;
}

interface WebSocketState {
	isConnected: boolean;
	isConnecting: boolean;
	error: Event | null;
}

// Ping 메시지 타입 (서버에서 무시)
const PING_MESSAGE = JSON.stringify({ type: "ping" });

export function useWebSocket<T = unknown>(
	url: string,
	options: WebSocketOptions<T> = {},
) {
	const {
		autoConnect = true,
		reconnectAttempts = CONFIG.websocket.reconnectAttempts,
		reconnectInterval = CONFIG.websocket.reconnectIntervalMs,
		enablePing = true,
	} = options;

	const [state, setState] = useState<WebSocketState>({
		isConnected: false,
		isConnecting: false,
		error: null,
	});

	const wsRef = useRef<WebSocket | null>(null);
	const reconnectCountRef = useRef(0);
	const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const pingTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
	const isMountedRef = useRef(true);

	// 콜백을 Ref에 저장하여 렌더링 사이클에서 분리 (Latest Ref Pattern)
	const parseMessageRef = useRef(options.parseMessage);
	const onMessageRef = useRef(options.onMessage);
	const onOpenRef = useRef(options.onOpen);
	const onCloseRef = useRef(options.onClose);
	const onErrorRef = useRef(options.onError);

	// 매 렌더링마다 최신 콜백으로 Ref 업데이트
	useEffect(() => {
		parseMessageRef.current = options.parseMessage;
		onMessageRef.current = options.onMessage;
		onOpenRef.current = options.onOpen;
		onCloseRef.current = options.onClose;
		onErrorRef.current = options.onError;
	}, [
		options.parseMessage,
		options.onMessage,
		options.onOpen,
		options.onClose,
		options.onError,
	]);

	const tryParseJson = (data: string): unknown => {
		try {
			return JSON.parse(data) as unknown;
		} catch {
			return data;
		}
	};

	// Ping 타이머 시작
	const startPingTimer = useCallback(() => {
		if (!enablePing) return;

		// 기존 타이머 정리
		if (pingTimerRef.current) {
			clearInterval(pingTimerRef.current);
		}

		pingTimerRef.current = setInterval(() => {
			if (wsRef.current?.readyState === WebSocket.OPEN) {
				wsRef.current.send(PING_MESSAGE);
			}
		}, CONFIG.websocket.pingIntervalMs);
	}, [enablePing]);

	// Ping 타이머 정지
	const stopPingTimer = useCallback(() => {
		if (pingTimerRef.current) {
			clearInterval(pingTimerRef.current);
			pingTimerRef.current = null;
		}
	}, []);

	const connect = useCallback(() => {
		if (!url) return;

		if (wsRef.current?.readyState === WebSocket.OPEN) {
			return;
		}

		if (wsRef.current) {
			wsRef.current.close();
		}

		setState((prev) => ({ ...prev, isConnecting: true, error: null }));

		try {
			const ws = new WebSocket(url);
			wsRef.current = ws;

			ws.onopen = () => {
				if (!isMountedRef.current) return;
				setState((prev) => ({
					...prev,
					isConnected: true,
					isConnecting: false,
				}));
				reconnectCountRef.current = 0;
				startPingTimer();
				onOpenRef.current?.();
			};

			ws.onmessage = (event) => {
				if (!isMountedRef.current) return;
				try {
					const rawData = event.data as unknown;
					const decodedData =
						typeof rawData === "string" ? tryParseJson(rawData) : rawData;

					// pong 메시지 무시
					if (
						typeof decodedData === "object" &&
						decodedData !== null &&
						"type" in decodedData
					) {
						const msgType = (decodedData as { type?: string }).type;
						if (msgType === "pong") return;
					}

					const parsed = parseMessageRef.current
						? parseMessageRef.current(decodedData)
						: (decodedData as T);

					if (parsed === null) return;

					onMessageRef.current?.(parsed);
				} catch (e) {
					console.error("WebSocket message processing error:", e);
				}
			};

			ws.onclose = () => {
				if (!isMountedRef.current) return;
				stopPingTimer();
				setState((prev) => ({
					...prev,
					isConnected: false,
					isConnecting: false,
				}));
				onCloseRef.current?.();

				if (autoConnect && reconnectCountRef.current < reconnectAttempts) {
					// Exponential Backoff: baseInterval * 2^retryCount (최대 CONFIG.websocket.maxBackoffMs)
					const backoffDelay = Math.min(
						reconnectInterval * 2 ** reconnectCountRef.current,
						CONFIG.websocket.maxBackoffMs,
					);
					reconnectTimerRef.current = setTimeout(() => {
						reconnectCountRef.current += 1;
						if (isMountedRef.current) connect();
					}, backoffDelay);
				}
			};

			ws.onerror = (event) => {
				if (!isMountedRef.current) return;
				setState((prev) => ({ ...prev, error: event }));
				onErrorRef.current?.(event);
			};
		} catch (e) {
			if (isMountedRef.current) {
				setState((prev) => ({ ...prev, isConnecting: false }));
			}
			console.error("WebSocket connection error:", e);
		}
		// 의존성 배열에서 콜백 제거 → connect 함수 안정화
	}, [
		url,
		autoConnect,
		reconnectAttempts,
		reconnectInterval,
		startPingTimer,
		stopPingTimer,
	]);

	const disconnect = useCallback(() => {
		stopPingTimer();
		if (reconnectTimerRef.current) {
			clearTimeout(reconnectTimerRef.current);
			reconnectTimerRef.current = null;
		}
		reconnectCountRef.current = 0;
		if (wsRef.current) {
			wsRef.current.close();
			wsRef.current = null;
		}
	}, [stopPingTimer]);

	useEffect(() => {
		isMountedRef.current = true;
		if (autoConnect && url) {
			connect();
		}
		return () => {
			isMountedRef.current = false;
			disconnect();
		};
	}, [connect, disconnect, autoConnect, url]);

	return {
		...state,
		connect,
		disconnect,
		sendMessage: (msg: string | object) => {
			if (wsRef.current?.readyState === WebSocket.OPEN) {
				wsRef.current.send(typeof msg === "string" ? msg : JSON.stringify(msg));
			}
		},
	};
}
