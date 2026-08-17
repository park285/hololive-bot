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

export async function createFetchTransport(options) {
  const proxyURL = validateProxy(options.proxy);
  if (!options.proxy.enabled) {
    return {
      fetch: effectiveFetch(options.currentSignal),
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
    fetch: effectiveFetch(options.currentSignal, undici.fetch, proxyAgent),
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

function effectiveFetch(currentSignal, proxyFetch, proxyAgent) {
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
    try {
      let response;
      if (proxyFetch == null || proxyAgent == null) {
        response = await globalThis.fetch(effective, { signal: combinedSignal });
      } else {
        const proxyInit = {
          method: effective.method,
          headers: Array.from(effective.headers.entries()),
          body: effective.body,
          redirect: effective.redirect,
          signal: combinedSignal,
          dispatcher: proxyAgent,
          ...(effective.body == null ? {} : { duplex: "half" }),
        };
        response = await proxyFetch(effective.url, proxyInit);
      }
      return await classifyUpstreamResponse(response);
    } catch (error) {
      if (combinedSignal.aborted) {
        throw abortError(requestSignal, effective.signal);
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
  };
}

async function classifyUpstreamResponse(response) {
  if (response.status < 500 || response.status > 599) {
    return response;
  }
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
  throw new FetchTransportError(
    "collection_failed",
    "TRANSIENT",
    `upstream request failed with status code ${response.status}`,
  );
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
