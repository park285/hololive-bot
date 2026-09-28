export class FetchTransportError extends Error {
  name: "FetchTransportError";
  code: string;
  failureClass: string;
  constructor(
    code: string,
    failureClass: string,
    message: string,
    options?: { cause?: unknown },
  );
}

export interface FetchTransport {
  fetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  /** 재시도 없이 요청 1회만 보냅니다. 같은 proxy agent와 요청 취소 신호를 공유합니다. */
  singleAttemptFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  close(): Promise<void>;
  agentCount: number;
}

export function createFetchTransport(options: {
  proxy: { enabled: boolean; url?: string };
  currentSignal: () => AbortSignal | undefined;
  loadUndici?: () => Promise<typeof import("undici")>;
  closeTimeoutMs?: number;
  retryDelayMs?: number;
  observeRetry?: (event: {
    endpoint: "browse" | "next" | "player";
    reason: "network" | "http_status";
    statusCode?: number;
    delayMs: number;
    attempt: number;
    maxAttempts: number;
  }) => void;
}): Promise<FetchTransport>;

export function redactedProxyURL(raw: string): string;
