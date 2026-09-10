import { useCallback, useEffect, useRef, useState } from "react";
import { CONFIG } from "@/config/constants";

interface WebSocketOptions<T> {
	protocol: string;
	beforeConnect: (signal: AbortSignal) => Promise<void>;
	canConnect: () => boolean;
	parseMessage: (data: unknown) => T | null;
	onMessage?: (data: T) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (event: Event) => void;
	autoConnect?: boolean;
	reconnectAttempts?: number;
	reconnectInterval?: number;
}

interface WebSocketState {
	isConnected: boolean;
	isConnecting: boolean;
	error: Event | null;
}

/** useWebSocket은 세대 확인 뒤 구독하며 모든 데이터 프레임을 호출자의 계약 검증에 넘깁니다. */
export function useWebSocket<T = unknown>(
	url: string,
	options: WebSocketOptions<T>,
) {
	const {
		autoConnect = true,
		reconnectAttempts = CONFIG.websocket.reconnectAttempts,
		reconnectInterval = CONFIG.websocket.reconnectIntervalMs,
	} = options;

	const [state, setState] = useState<WebSocketState>({
		isConnected: false,
		isConnecting: false,
		error: null,
	});

	const wsRef = useRef<WebSocket | null>(null);
	const preflightRef = useRef<AbortController | null>(null);
	const reconnectCountRef = useRef(0);
	const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const isMountedRef = useRef(true);

	const parseMessageRef = useRef(options.parseMessage);
	const onMessageRef = useRef(options.onMessage);
	const onOpenRef = useRef(options.onOpen);
	const onCloseRef = useRef(options.onClose);
	const onErrorRef = useRef(options.onError);
	const beforeConnectRef = useRef(options.beforeConnect);
	const canConnectRef = useRef(options.canConnect);

	useEffect(() => {
		parseMessageRef.current = options.parseMessage;
		onMessageRef.current = options.onMessage;
		onOpenRef.current = options.onOpen;
		onCloseRef.current = options.onClose;
		onErrorRef.current = options.onError;
		beforeConnectRef.current = options.beforeConnect;
		canConnectRef.current = options.canConnect;
	}, [
		options.parseMessage,
		options.onMessage,
		options.onOpen,
		options.onClose,
		options.onError,
		options.beforeConnect,
		options.canConnect,
	]);

	const tryParseJson = (data: string): unknown => {
		try {
			return JSON.parse(data) as unknown;
		} catch {
			return null;
		}
	};

	const clearReconnectTimer = useCallback(() => {
		if (reconnectTimerRef.current) {
			clearTimeout(reconnectTimerRef.current);
			reconnectTimerRef.current = null;
		}
	}, []);

	const connect = useCallback(async (): Promise<void> => {
		if (!url || !canConnectRef.current() || preflightRef.current !== null) return;

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

		const preflight = new AbortController();
		preflightRef.current = preflight;
		const scheduleReconnect = () => {
			if (!autoConnect || !canConnectRef.current() || reconnectCountRef.current >= reconnectAttempts) return;
			const backoffDelay = Math.min(reconnectInterval * 2 ** reconnectCountRef.current, CONFIG.websocket.maxBackoffMs);
			reconnectTimerRef.current = setTimeout(() => {
				reconnectTimerRef.current = null;
				if (!isMountedRef.current || wsRef.current !== null) return;
				reconnectCountRef.current += 1;
				void connect();
			}, backoffDelay);
		};
		try {
			await beforeConnectRef.current(preflight.signal);
			if (!isMountedRef.current || preflightRef.current !== preflight || preflight.signal.aborted || !canConnectRef.current()) return;
			const ws = new WebSocket(url, [options.protocol]);
			wsRef.current = ws;
			let negotiated = false;

			ws.onopen = () => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
				if (ws.protocol !== options.protocol) { ws.close(); return; }
				negotiated = true;
				setState((prev) => ({
					...prev,
					isConnected: true,
					isConnecting: false,
				}));
				reconnectCountRef.current = 0;
				onOpenRef.current?.();
			};

			ws.onmessage = (event) => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
				if (!negotiated || !canConnectRef.current()) return;
				try {
					const rawData = event.data as unknown;
					const decodedData =
						typeof rawData === "string" ? tryParseJson(rawData) : rawData;

					const parsed = parseMessageRef.current(decodedData);

					if (parsed === null) return;

					onMessageRef.current?.(parsed);
				} catch {
					const error = new Event("messageerror");
					setState(prev => ({ ...prev, error }));
					onErrorRef.current?.(error);
				}
			};

			ws.onclose = () => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
				wsRef.current = null;
				setState((prev) => ({
					...prev,
					isConnected: false,
					isConnecting: false,
				}));
				onCloseRef.current?.();

				scheduleReconnect();
			};

			ws.onerror = (event) => {
				if (!isMountedRef.current || wsRef.current !== ws) return;
				setState((prev) => ({ ...prev, error: event }));
				onErrorRef.current?.(event);
			};
		} catch {
			if (isMountedRef.current && !preflight.signal.aborted) {
				const error = new Event("error");
				setState((prev) => ({ ...prev, isConnecting: false, error }));
				onErrorRef.current?.(error);
				scheduleReconnect();
			}
		} finally {
			if (preflightRef.current === preflight) preflightRef.current = null;
		}
	}, [
		url,
		autoConnect,
		clearReconnectTimer,
		reconnectAttempts,
		reconnectInterval,
		options.protocol,
	]);

	const disconnect = useCallback(() => {
		preflightRef.current?.abort();
		preflightRef.current = null;
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
	}, [clearReconnectTimer]);

	useEffect(() => {
		isMountedRef.current = true;
		return () => {
			isMountedRef.current = false;
		};
	}, []);

	useEffect(() => {
		if (autoConnect && url) {
			void connect();
		}
		return () => {
			disconnect();
		};
	}, [connect, disconnect, autoConnect, url]);

	return {
		...state,
		connect,
		disconnect,
	};
}
