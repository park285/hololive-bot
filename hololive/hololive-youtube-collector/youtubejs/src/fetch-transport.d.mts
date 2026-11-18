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
  singleAttemptFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
}

export function createFetchTransport(options: {
  currentSignal: () => AbortSignal | undefined;
  maxResponseBodyBytes?: number;
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
