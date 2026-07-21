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
	enablePing?: boolean;
}

interface WebSocketState {
	isConnected: boolean;
	isConnecting: boolean;
	error: Event | null;
}

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

	const parseMessageRef = useRef(options.parseMessage);
	const onMessageRef = useRef(options.onMessage);
	const onOpenRef = useRef(options.onOpen);
	const onCloseRef = useRef(options.onClose);
	const onErrorRef = useRef(options.onError);

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

	const startPingTimer = useCallback(() => {
		if (!enablePing) return;

		if (pingTimerRef.current) {
			clearInterval(pingTimerRef.current);
		}

		pingTimerRef.current = setInterval(() => {
			if (wsRef.current?.readyState === WebSocket.OPEN) {
				wsRef.current.send(PING_MESSAGE);
			}
		}, CONFIG.websocket.pingIntervalMs);
	}, [enablePing]);

	const stopPingTimer = useCallback(() => {
		if (pingTimerRef.current) {
			clearInterval(pingTimerRef.current);
			pingTimerRef.current = null;
		}
	}, []);

	const clearReconnectTimer = useCallback(() => {
		if (reconnectTimerRef.current) {
			clearTimeout(reconnectTimerRef.current);
			reconnectTimerRef.current = null;
		}
	}, []);

	const connect = useCallback(() => {
		if (!url) return;

		clearReconnectTimer();
		if (
			wsRef.current?.readyState === WebSocket.OPEN ||
			wsRef.current?.readyState === WebSocket.CONNECTING
		) {
			return;
		}

		if (wsRef.current) {
			const previous = wsRef.current;
			wsRef.current = null;
			previous.close();
		}

		setState((prev) => ({
			...prev,
			isConnected: false,
			isConnecting: true,
			error: null,
		}));

		try {
			const ws = new WebSocket(url);
			wsRef.current = ws;

			ws.onopen = () => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
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
				if (!isMountedRef.current || wsRef.current !== ws) return;
				try {
					const rawData = event.data as unknown;
					const decodedData =
						typeof rawData === "string" ? tryParseJson(rawData) : rawData;

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
				if (!isMountedRef.current || wsRef.current !== ws) return;
				wsRef.current = null;
				stopPingTimer();
				setState((prev) => ({
					...prev,
					isConnected: false,
					isConnecting: false,
				}));
				onCloseRef.current?.();

				if (autoConnect && reconnectCountRef.current < reconnectAttempts) {
					const backoffDelay = Math.min(
						reconnectInterval * 2 ** reconnectCountRef.current,
						CONFIG.websocket.maxBackoffMs,
					);
					reconnectTimerRef.current = setTimeout(() => {
						reconnectTimerRef.current = null;
						if (!isMountedRef.current || wsRef.current !== null) return;
						reconnectCountRef.current += 1;
						connect();
					}, backoffDelay);
				}
			};

			ws.onerror = (event) => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
				setState((prev) => ({ ...prev, error: event }));
				onErrorRef.current?.(event);
			};
		} catch (e) {
			if (isMountedRef.current) {
				setState((prev) => ({ ...prev, isConnecting: false }));
			}
			console.error("WebSocket connection error:", e);
		}
	}, [
		url,
		autoConnect,
		clearReconnectTimer,
		reconnectAttempts,
		reconnectInterval,
		startPingTimer,
		stopPingTimer,
	]);

	const disconnect = useCallback(() => {
		stopPingTimer();
		clearReconnectTimer();
		reconnectCountRef.current = 0;
		const ws = wsRef.current;
		wsRef.current = null;
		ws?.close();
		if (isMountedRef.current) {
			setState((prev) => ({
				...prev,
				isConnected: false,
				isConnecting: false,
			}));
			if (ws) onCloseRef.current?.();
		}
	}, [clearReconnectTimer, stopPingTimer]);

	useEffect(() => {
		isMountedRef.current = true;
		return () => {
			isMountedRef.current = false;
		};
	}, []);

	useEffect(() => {
		if (autoConnect && url) {
			connect();
		}
		return () => {
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
