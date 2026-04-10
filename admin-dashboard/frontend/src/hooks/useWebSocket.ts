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
