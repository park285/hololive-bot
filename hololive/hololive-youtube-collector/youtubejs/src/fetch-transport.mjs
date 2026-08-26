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
const retryableStatusCodes = new Set([500, 502, 503, 504]);
const retryableYouTubePaths = new Map([
  ["/youtubei/v1/browse", "browse"],
  ["/youtubei/v1/next", "next"],
  ["/youtubei/v1/player", "player"],
]);
const retryDelayMinMs = 100;
const retryDelayMaxMs = 300;

export async function createFetchTransport(options) {
  const proxyURL = validateProxy(options.proxy);
  const retryOptions = {
    delayMs: validateRetryDelay(options.retryDelayMs),
    observe: typeof options.observeRetry === "function" ? options.observeRetry : logRetryEvent,
  };
  if (!options.proxy.enabled) {
    return {
      fetch: effectiveFetch(options.currentSignal, undefined, undefined, retryOptions),
      async close() {},
      agentCount: 0,
    };
  }

  const undici = await (options.loadUndici ?? (() => import("undici")))();
  const { ProxyAgent } = undici;
  let proxyAgent;
  try {
    proxyAgent = new ProxyAgent(proxyURL);
  } catch (error) {
    throw new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      `proxy transport construction failed for ${redactedProxyURL(proxyURL)}`,
      { cause: error },
    );
  }
  let closePromise;
  let destroyed = false;
  return {
    fetch: effectiveFetch(options.currentSignal, undici.fetch, proxyAgent, retryOptions),
    close() {
      closePromise ??= closeProxyAgent(proxyAgent, options.closeTimeoutMs, () => {
        if (!destroyed) {
          destroyed = true;
          proxyAgent.destroy(new Error("proxy transport close failed"));
        }
      });
      return closePromise;
    },
    agentCount: 1,
  };
}

function effectiveFetch(currentSignal, proxyFetch, proxyAgent, retryOptions) {
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
    const retry = retryRequest(effective);
    let request = effective;
    let retryAttempted = false;
    while (true) {
      let response;
      try {
        response = await performFetch(request, combinedSignal, proxyFetch, proxyAgent);
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

async function performFetch(request, signal, proxyFetch, proxyAgent) {
  if (proxyFetch == null || proxyAgent == null) {
    return globalThis.fetch(request, { signal });
  }
  const proxyInit = {
    method: request.method,
    headers: Array.from(request.headers.entries()),
    body: request.body,
    redirect: request.redirect,
    signal,
    dispatcher: proxyAgent,
    ...(request.body == null ? {} : { duplex: "half" }),
  };
  return proxyFetch(request.url, proxyInit);
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

async function closeProxyAgent(proxyAgent, closeTimeoutMs, destroy) {
  const timeout = Number.isFinite(closeTimeoutMs) && Number(closeTimeoutMs) > 0
    ? Number(closeTimeoutMs)
    : 3_000;
  let timer;
  try {
    await Promise.race([
      proxyAgent.close(),
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error("proxy transport close timed out")), timeout);
      }),
    ]);
  } catch (error) {
    destroy();
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

function validateProxy(proxy) {
  const raw = typeof proxy.url === "string" ? proxy.url.trim() : "";
  if (!proxy.enabled) {
    if (raw !== "") {
      throw new FetchTransportError("helper_protocol_mismatch", "PROTOCOL", "proxy url must be empty when disabled");
    }
    return "";
  }
  if (raw === "") {
    throw new FetchTransportError("helper_protocol_mismatch", "PROTOCOL", "proxy url is required when enabled");
  }
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new FetchTransportError("helper_protocol_mismatch", "PROTOCOL", "proxy url is invalid");
  }
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    parsed.hostname === "" ||
    (parsed.pathname !== "" && parsed.pathname !== "/") ||
    parsed.search !== "" ||
    parsed.hash !== ""
  ) {
    throw new FetchTransportError("helper_protocol_mismatch", "PROTOCOL", "proxy url is invalid");
  }
  return raw;
}

export function redactedProxyURL(raw) {
  try {
    const parsed = new URL(raw);
    parsed.username = "";
    parsed.password = "";
    return parsed.origin + (parsed.pathname === "/" ? "" : parsed.pathname);
  } catch {
    return "[redacted-proxy]";
  }
}
