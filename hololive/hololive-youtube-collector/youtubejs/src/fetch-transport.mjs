import { classifyUpstreamError, failureTuples, isTransientNetworkError, upstreamHTTPFailureCode } from "./upstream-errors.mjs";

export class FetchTransportError extends Error {
  constructor(code, failureClass, message, options) {
    super(message, options);
    this.name = "FetchTransportError";
    this.code = code;
    this.failureClass = failureClass;
  }
}

const retryableStatusCodes = new Set([500, 503]);
const retryableYouTubePaths = new Map([
  ["/youtubei/v1/browse", "browse"],
  ["/youtubei/v1/next", "next"],
  ["/youtubei/v1/player", "player"],
]);
const retryDelayMinMs = 100;
const retryDelayMaxMs = 300;
const defaultMaxResponseBodyBytes = 8 * 1024 * 1024;

export function createFetchTransport(options) {
  const retryOptions = {
    delayMs: validateRetryDelay(options.retryDelayMs),
    observe: typeof options.observeRetry === "function" ? options.observeRetry : logRetryEvent,
  };
  const maxResponseBodyBytes = validateResponseBodyLimit(options.maxResponseBodyBytes);
  return {
    fetch: effectiveFetch(options.currentSignal, retryOptions, maxResponseBodyBytes),
    singleAttemptFetch: effectiveFetch(options.currentSignal, null, maxResponseBodyBytes),
  };
}

function effectiveFetch(currentSignal, retryOptions, maxResponseBodyBytes) {
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
        if (error instanceof FetchTransportError) throw error;
        const code = classifyUpstreamError(error, "upstream");
        throw new FetchTransportError(code, failureTuples[code].class, "upstream request failed", { cause: error });
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

      return await classifyUpstreamResponse(response, maxResponseBodyBytes);
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

function validateResponseBodyLimit(value) {
  const limit = value ?? defaultMaxResponseBodyBytes;
  if (!Number.isSafeInteger(limit) || limit <= 0) {
    throw new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      "fetch response body limit is invalid",
    );
  }
  return limit;
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

async function classifyUpstreamResponse(response, maxResponseBodyBytes) {
  if (response.status !== 429 && (response.status < 500 || response.status > 599)) {
    return await limitUpstreamResponse(response, maxResponseBodyBytes);
  }
  await discardUpstreamResponse(response);
  const code = upstreamHTTPFailureCode(response.status);
  throw new FetchTransportError(code, failureTuples[code].class, `upstream request failed with status code ${response.status}`);
}

async function limitUpstreamResponse(response, maxResponseBodyBytes) {
  const declaredLength = declaredResponseLength(response.headers.get("content-length"));
  if (declaredLength !== undefined && declaredLength > maxResponseBodyBytes) {
    try {
      await response.body?.cancel();
    } catch {}
    throw responseTooLargeError();
  }
  if (response.body == null) return response;
  return preserveResponseMetadata(
    new Response(limitedResponseBody(response.body, maxResponseBodyBytes), {
      headers: response.headers,
      status: response.status,
      statusText: response.statusText,
    }),
    response,
  );
}

function declaredResponseLength(value) {
  if (value == null || !/^\d+$/u.test(value)) return undefined;
  const length = Number(value);
  return Number.isSafeInteger(length) ? length : Number.POSITIVE_INFINITY;
}

function limitedResponseBody(body, maxResponseBodyBytes) {
  const reader = body.getReader();
  let receivedBytes = 0;
  let stopped = false;
  return new ReadableStream({
    async pull(controller) {
      try {
        const chunk = await reader.read();
        if (stopped) return;
        if (chunk.done) {
          stopped = true;
          controller.close();
          return;
        }
        receivedBytes += chunk.value.byteLength;
        if (receivedBytes > maxResponseBodyBytes) {
          stopped = true;
          const error = responseTooLargeError();
          controller.error(error);
          void reader.cancel(error).catch(() => {});
          return;
        }
        controller.enqueue(chunk.value);
      } catch (error) {
        if (stopped) return;
        stopped = true;
        controller.error(error);
      }
    },
    cancel(reason) {
      stopped = true;
      return reader.cancel(reason);
    },
  });
}

function preserveResponseMetadata(response, source) {
  const clone = response.clone.bind(response);
  Object.defineProperties(response, {
    url: { configurable: true, enumerable: true, value: source.url },
    redirected: { configurable: true, enumerable: true, value: source.redirected },
    type: { configurable: true, enumerable: true, value: source.type },
    clone: {
      configurable: true,
      enumerable: true,
      writable: true,
      value: () => preserveResponseMetadata(clone(), source),
    },
  });
  return response;
}

function responseTooLargeError() {
  return new FetchTransportError(
    "response_too_large",
    failureTuples.response_too_large.class,
    "upstream response exceeds body limit",
  );
}

async function discardUpstreamResponse(response) {
  try {
    await response.body?.cancel();
  } catch (error) {
    if (isTransientNetworkError(error)) {
      const code = upstreamHTTPFailureCode(response.status);
      throw new FetchTransportError(code, failureTuples[code].class, "upstream response cleanup failed", { cause: error });
    }
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
