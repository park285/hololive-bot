// @ts-check

import { AsyncLocalStorage } from "node:async_hooks";
import { Parser, Utils } from "youtubei.js";
import { currentRequestSignal } from "./request-context.mjs";

/** @typedef {import("./contracts.d.ts").RPCErrorCode} RPCErrorCode */

export const failureTuples = Object.freeze({
  invalid_request: Object.freeze({ class: "PROTOCOL", statuses: Object.freeze([400, 404]) }),
  request_too_large: Object.freeze({ class: "PROTOCOL", statuses: Object.freeze([413]) }),
  helper_not_ready: Object.freeze({ class: "PROTOCOL", statuses: Object.freeze([503]) }),
  helper_busy: Object.freeze({ class: "TRANSIENT", statuses: Object.freeze([503]) }),
  collection_canceled: Object.freeze({ class: "CANCELED", statuses: Object.freeze([408]) }),
  collection_timeout: Object.freeze({ class: "TIMEOUT", statuses: Object.freeze([504, 408]) }),
  cooldown: Object.freeze({ class: "COOLDOWN", statuses: Object.freeze([429]) }),
  parser_drift: Object.freeze({ class: "DATA_CONTRACT", statuses: Object.freeze([422]) }),
  configuration_error: Object.freeze({ class: "CONFIGURATION", statuses: Object.freeze([502]) }),
  response_too_large: Object.freeze({ class: "RESOURCE_LIMIT", statuses: Object.freeze([422]) }),
  helper_protocol_mismatch: Object.freeze({ class: "PROTOCOL", statuses: Object.freeze([409]) }),
  helper_internal_invariant: Object.freeze({ class: "INTERNAL", statuses: Object.freeze([500]) }),
  collection_failed: Object.freeze({ class: "TRANSIENT", statuses: Object.freeze([502]) }),
});

/** @type {AsyncLocalStorage<{ failure?: unknown }>} */
const parserFailures = new AsyncLocalStorage();

Parser.setParserErrorHandler((failure) => {
  const scope = parserFailures.getStore();
  if (scope == null || failure.error_type === "class_not_found" || failure.error_type === "class_changed") {
    return;
  }
  const cause = failure.error_type === "parse" ? failure.error : undefined;
  if (isCriticalFailure(cause)) {
    if (!isCriticalFailure(scope.failure)) scope.failure = cause;
    return;
  }
  scope.failure ??= Object.assign(new Error("upstream parser rejected response data"), {
    code: "parser_drift", failureClass: "DATA_CONTRACT",
  });
});

const upstreamStatusMessagePattern = /^Request to .* failed with status code ([1-5]\d{2})$/;

const transientNetworkCodes = new Set([
  "ECONNRESET", "ENETRESET", "EPIPE", "ETIMEDOUT", "EAI_AGAIN",
  "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT", "UND_ERR_SOCKET",
]);

/** @param {unknown} error */
export function isTransientNetworkError(error) {
  if (!isRecord(error)) return false;
  if (typeof error.code === "string" && transientNetworkCodes.has(error.code)) return true;
  return isRecord(error.cause) && typeof error.cause.code === "string" && transientNetworkCodes.has(error.cause.code);
}

/**
 * @param {number} status
 * @returns {RPCErrorCode}
 */
export function upstreamHTTPFailureCode(status) {
  if (status === 401 || status === 403) return "configuration_error";
  if (status === 429) return "cooldown";
  return "collection_failed";
}

/**
 * @param {unknown} error
 * @returns {number | undefined}
 */
export function upstreamHTTPStatus(error) {
  if (error instanceof Utils.InnertubeError) {
    const match = upstreamStatusMessagePattern.exec(error.message);
    return match == null ? undefined : Number(match[1]);
  }
  if (isRecord(error) && typeof error.status === "number" && Number.isInteger(error.status) && error.status >= 400 && error.status <= 599) {
    return error.status;
  }
  return undefined;
}

/**
 * @param {unknown} error
 * @param {"helper" | "upstream"} [origin]
 * @returns {RPCErrorCode}
 */
export function classifyUpstreamError(error, origin = "helper") {
  if (currentRequestSignal()?.aborted) return "collection_canceled";
  if (isRecord(error) && typeof error.code === "string" && Object.hasOwn(failureTuples, error.code)) {
    return /** @type {RPCErrorCode} */ (error.code);
  }
  if (isTransientNetworkError(error)) return "collection_failed";
  const status = upstreamHTTPStatus(error);
  if (status !== undefined) return upstreamHTTPFailureCode(status);
  if (error instanceof Utils.InnertubeError) return "collection_failed";
  if (error instanceof TypeError && error.message === "fetch failed") return "collection_failed";
  if (error instanceof Error && error.name === "AbortError") return "helper_internal_invariant";
  if (origin === "helper" && isProgrammingError(error)) {
    return "helper_internal_invariant";
  }
  return "collection_failed";
}

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

/**
 * @template T
 * @param {() => Promise<T>} operation
 * @returns {Promise<T>}
 */
export async function runUpstream(operation) {
  /** @type {{ failure?: unknown }} */
  const scope = {};
  return parserFailures.run(scope, async () => {
    try {
      const result = await operation();
      if (scope.failure !== undefined) throw scope.failure;
      return result;
    } catch (error) {
      throw scopedUpstreamFailure(error, scope.failure);
    }
  });
}

/**
 * @template T
 * @param {() => T} read
 * @returns {T}
 */
export function readUpstream(read) {
  /** @type {{ failure?: unknown }} */
  const scope = {};
  return parserFailures.run(scope, () => {
    try {
      const result = read();
      if (scope.failure !== undefined) throw scope.failure;
      return result;
    } catch (error) {
      throw scopedUpstreamFailure(error, scope.failure);
    }
  });
}

/**
 * @param {unknown} error
 * @param {unknown} parserFailure
 */
function scopedUpstreamFailure(error, parserFailure) {
  if (isCriticalFailure(error)) return error;
  return parserFailure ?? upstreamFailure(error);
}

/** @param {unknown} error */
function isCriticalFailure(error) {
  if (error instanceof Error && error.name === "AbortError") return true;
  if (!isRecord(error) || typeof error.code !== "string" || !Object.hasOwn(failureTuples, error.code)) return false;
  const tuple = failureTuples[/** @type {RPCErrorCode} */ (error.code)];
  return tuple.class === "INTERNAL" || tuple.class === "PROTOCOL" || tuple.class === "CANCELED";
}

/** @param {unknown} error */
function upstreamFailure(error) {
  if (isProgrammingError(error) && classifyUpstreamError(error, "upstream") === "collection_failed") {
    return Object.assign(new Error("upstream response processing failed", { cause: error }), {
      code: "collection_failed", failureClass: "TRANSIENT",
    });
  }
  return error;
}

/** @param {unknown} error */
function isProgrammingError(error) {
  return error instanceof TypeError || error instanceof ReferenceError || error instanceof RangeError ||
    error instanceof SyntaxError || error instanceof URIError || error instanceof EvalError;
}
