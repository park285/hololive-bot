export class FetchTransportError extends Error {
  constructor(code, failureClass, message, options) {
    super(message, options);
    this.name = "FetchTransportError";
    this.code = code;
    this.failureClass = failureClass;
  }
}

const transientNetworkCodes = new Set([
  "ECONNRESET",
  "ENETRESET",
  "EPIPE",
  "ETIMEDOUT",
  "EAI_AGAIN",
  "UND_ERR_CONNECT_TIMEOUT",
  "UND_ERR_HEADERS_TIMEOUT",
  "UND_ERR_BODY_TIMEOUT",
  "UND_ERR_SOCKET",
]);
const retryableStatusCodes = new Set([500, 503]);
const retryableYouTubePaths = new Map([
  ["/youtubei/v1/browse", "browse"],
  ["/youtubei/v1/next", "next"],
  ["/youtubei/v1/player", "player"],
]);
const retryDelayMinMs = 100;
const retryDelayMaxMs = 300;

export function createFetchTransport(options) {
  const retryOptions = {
    delayMs: validateRetryDelay(options.retryDelayMs),
    observe: typeof options.observeRetry === "function" ? options.observeRetry : logRetryEvent,
  };
  return {
    fetch: effectiveFetch(options.currentSignal, retryOptions),
    // 라이브 확인 RPC는 물리 요청 상한을 지키도록 같은 요청 취소 신호를 쓰되 재시도하지 않습니다.
    singleAttemptFetch: effectiveFetch(options.currentSignal, null),
  };
}

function effectiveFetch(currentSignal, retryOptions) {
  return async (input, init) => {
    if (input instanceof Request && (input.bodyUsed || input.body?.locked)) {
      throw new FetchTransportError(
        "helper_internal_invariant",
        "INTERNAL",
        "fetch request body is locked or disturbed",
      );
    }
    let effective;
    try {
      effective = new globalThis.Request(input, init);
      // 자동 redirect도 추가 물리 요청이므로 새 확인의 단일 시도 경로에서는 허용하지 않습니다.
      if (retryOptions == null) effective = new globalThis.Request(effective, { redirect: "error" });
    } catch (error) {
      throw new FetchTransportError(
        "helper_protocol_mismatch",
        "PROTOCOL",
        "fetch request is invalid",
        { cause: error },
      );
    }
    const requestSignal = currentSignal();
    const combinedSignal = requestSignal == null
      ? effective.signal
      : AbortSignal.any([effective.signal, requestSignal]);
    if (combinedSignal.aborted) {
      throw abortError(requestSignal, effective.signal);
    }
    const retry = retryOptions == null ? { endpoint: "", request: null } : retryRequest(effective);
    let request = effective;
    let retryAttempted = false;
    while (true) {
      let response;
      try {
        response = await globalThis.fetch(request, { signal: combinedSignal });
      } catch (error) {
        if (combinedSignal.aborted) {
          throw abortError(requestSignal, effective.signal);
        }
        if (!retryAttempted && retry.request != null && isTransientNetworkError(error)) {
          retryAttempted = true;
          const delayMs = nextRetryDelay(retryOptions.delayMs);
          emitRetry(retryOptions.observe, {
            endpoint: retry.endpoint,
            reason: "network",
            delayMs,
            attempt: 2,
            maxAttempts: 2,
          });
          if (!await waitBeforeRetry(combinedSignal, delayMs)) {
            throw abortError(requestSignal, effective.signal);
          }
          request = retry.request;
          continue;
        }
        if (isTransientNetworkError(error)) {
          throw new FetchTransportError(
            "collection_failed",
            "TRANSIENT",
            "upstream request failed",
            { cause: error },
          );
        }
        throw error;
      }

      if (!retryAttempted && retry.request != null && retryableStatusCodes.has(response.status)) {
        await discardUpstreamResponse(response);
        retryAttempted = true;
        const delayMs = nextRetryDelay(retryOptions.delayMs);
        emitRetry(retryOptions.observe, {
          endpoint: retry.endpoint,
          reason: "http_status",
          statusCode: response.status,
          delayMs,
          attempt: 2,
          maxAttempts: 2,
        });
        if (!await waitBeforeRetry(combinedSignal, delayMs)) {
          throw abortError(requestSignal, effective.signal);
        }
        request = retry.request;
        continue;
      }

      return await classifyUpstreamResponse(response);
    }
  };
}

function retryRequest(request) {
  const endpoint = retryableEndpoint(request);
  if (endpoint === "") {
    return { endpoint, request: null };
  }
  try {
    return { endpoint, request: request.clone() };
  } catch {
    return { endpoint, request: null };
  }
}

function retryableEndpoint(request) {
  if (request.method !== "POST") {
    return "";
  }
  let parsed;
  try {
    parsed = new URL(request.url);
  } catch {
    return "";
  }
  if (parsed.protocol !== "https:" || parsed.hostname !== "www.youtube.com") {
    return "";
  }
  return retryableYouTubePaths.get(parsed.pathname) ?? "";
}

function validateRetryDelay(value) {
  if (value == null) {
    return undefined;
  }
  if (!Number.isSafeInteger(value) || value < 0 || value > 1_000) {
    throw new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      "fetch retry delay is invalid",
    );
  }
  return value;
}

function nextRetryDelay(override) {
  if (override != null) {
    return override;
  }
  return retryDelayMinMs + Math.floor(Math.random() * (retryDelayMaxMs - retryDelayMinMs + 1));
}

function emitRetry(observe, event) {
  try {
    observe(event);
  } catch {}
}

function logRetryEvent(event) {
  process.stderr.write(`${JSON.stringify({
    time: new Date().toISOString(),
    level: "INFO",
    source: "youtubejs/fetch-transport",
    msg: "YouTube.js upstream request retry scheduled",
    event: "youtubejs_upstream_retry_scheduled",
    endpoint: event.endpoint,
    reason: event.reason,
    ...(event.statusCode == null ? {} : { status_code: event.statusCode }),
    delay_ms: event.delayMs,
    attempt: event.attempt,
    max_attempts: event.maxAttempts,
  })}\n`);
}

async function waitBeforeRetry(signal, delayMs) {
  if (signal.aborted) {
    return false;
  }
  if (delayMs === 0) {
    return true;
  }
  return new Promise((resolve) => {
    let timer;
    const finish = (completed) => {
      clearTimeout(timer);
      signal.removeEventListener("abort", onAbort);
      resolve(completed);
    };
    const onAbort = () => finish(false);
    signal.addEventListener("abort", onAbort, { once: true });
    timer = setTimeout(() => finish(true), delayMs);
  });
}

async function classifyUpstreamResponse(response) {
  if (response.status === 429) {
    // youtubei.js가 HTTP status 없는 InnertubeError로 바꾸기 전에 기존 cooldown 계약을 보존합니다.
    await discardUpstreamResponse(response);
    throw new FetchTransportError(
      "cooldown",
      "COOLDOWN",
      "upstream request failed with status code 429",
    );
  }
  if (response.status < 500 || response.status > 599) {
    return response;
  }
  await discardUpstreamResponse(response);
  throw new FetchTransportError(
    "collection_failed",
    "TRANSIENT",
    `upstream request failed with status code ${response.status}`,
  );
}

async function discardUpstreamResponse(response) {
  try {
    await response.body?.cancel();
  } catch (error) {
    throw new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      "upstream response cleanup failed",
      { cause: error },
    );
  }
}

function abortError(requestSignal, requestInitSignal) {
  if (requestSignal?.aborted) {
    return new FetchTransportError("collection_canceled", "CANCELED", "collection canceled");
  }
  if (requestInitSignal.aborted) {
    return new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      "fetch request signal aborted outside request cancellation",
    );
  }
  return new FetchTransportError("helper_internal_invariant", "INTERNAL", "fetch aborted without provenance");
}

function isTransientNetworkError(error) {
  if (error == null || typeof error !== "object") {
    return false;
  }
  const code = "code" in error ? error.code : undefined;
  if (typeof code === "string" && transientNetworkCodes.has(code)) {
    return true;
  }
  const cause = "cause" in error ? error.cause : undefined;
  if (cause == null || typeof cause !== "object") {
    return false;
  }
  const causeCode = "code" in cause ? cause.code : undefined;
  return typeof causeCode === "string" && transientNetworkCodes.has(causeCode);
}
