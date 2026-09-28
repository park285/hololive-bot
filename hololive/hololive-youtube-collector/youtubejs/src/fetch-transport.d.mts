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
  /** 재시도 없이 요청 1회만 보냅니다. 같은 요청 취소 신호를 공유합니다. */
  singleAttemptFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
}

export function createFetchTransport(options: {
  currentSignal: () => AbortSignal | undefined;
  retryDelayMs?: number;
  observeRetry?: (event: {
    endpoint: "browse" | "next" | "player";
    reason: "network" | "http_status";
    statusCode?: number;
    delayMs: number;
    attempt: number;
    maxAttempts: number;
  }) => void;
}): FetchTransport;
