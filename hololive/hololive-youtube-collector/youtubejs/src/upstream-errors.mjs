// @ts-check

import { AsyncLocalStorage } from "node:async_hooks";
import { Parser, Utils } from "youtubei.js";
import { currentRequestSignal } from "./request-context.mjs";

/** @typedef {import("./contracts.d.ts").RPCErrorCode} RPCErrorCode */

// RPC 오류의 HTTP 상태·분류 조합을 한 곳에서 소유합니다.
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

// 전역 hook은 한 번만 설치하고 현재 호출 경계에만 실패를 남깁니다. 콜백에서
// 던지지 않아 라이브러리의 memo 정리가 끝난 뒤 결과를 거부합니다.
Parser.setParserErrorHandler((failure) => {
  const scope = parserFailures.getStore();
  if (scope == null || failure.error_type === "class_not_found" || failure.error_type === "class_changed") {
    // 소유한 호출 밖에는 상태를 남기지 않으며 성공한 JIT 진단은 실패가 아닙니다.
    return;
  }
  const cause = failure.error_type === "parse" ? failure.error : undefined;
  if (isCriticalFailure(cause)) {
    // 먼저 발생한 parser drift 뒤에 보고된 명시적 결함도 숨기지 않습니다.
    if (!isCriticalFailure(scope.failure)) scope.failure = cause;
    return;
  }
  // classdata와 원문 오류를 저장하거나 로그에 노출하지 않습니다.
  scope.failure ??= Object.assign(new Error("upstream parser rejected response data"), {
    code: "parser_drift", failureClass: "DATA_CONTRACT",
  });
});

const transientNetworkCodes = new Set([
  "ECONNRESET", "ENETRESET", "EPIPE", "ETIMEDOUT", "EAI_AGAIN",
  "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT", "UND_ERR_SOCKET",
]);

/** 헤더 수신 전 재시도에 이미 허용된 네트워크 오류만 판정합니다. 본문 오류를 재시도하지는 않습니다.
 * @param {unknown} error
 */
export function isTransientNetworkError(error) {
  if (!isRecord(error)) return false;
  if (typeof error.code === "string" && transientNetworkCodes.has(error.code)) return true;
  return isRecord(error.cause) && typeof error.cause.code === "string" && transientNetworkCodes.has(error.cause.code);
}

/** HTTP 실패를 기존 RPC 오류 코드로 매핑합니다.
 * @param {number} status @returns {RPCErrorCode}
 */
export function upstreamHTTPFailureCode(status) {
  if (status === 401 || status === 403) return "configuration_error";
  if (status === 429) return "cooldown";
  return "collection_failed";
}

/** 취소·명시적 helper 결함은 보존하고, 일반 upstream 실패는 비치명적 수집 실패로 분류합니다.
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
  if (error instanceof Utils.InnertubeError) {
    // youtubei.js 18의 HTTPClient는 status 필드 없이 이 메시지만 제공합니다.
    const match = /^Request to .* failed with status code ([1-5]\d{2})$/.exec(error.message);
    return match == null ? "collection_failed" : upstreamHTTPFailureCode(Number(match[1]));
  }
  if (isRecord(error) && typeof error.status === "number" && Number.isInteger(error.status) && error.status >= 400 && error.status <= 599) {
    return upstreamHTTPFailureCode(error.status);
  }
  // undici는 DNS·TLS·redirect 실패도 TypeError로 전달합니다. 이 분류는 재시도 허용 목록을 넓히지 않습니다.
  if (error instanceof TypeError && error.message === "fetch failed") return "collection_failed";
  if (error instanceof Error && error.name === "AbortError") return "helper_internal_invariant";
  if (origin === "helper" && isProgrammingError(error)) {
    return "helper_internal_invariant";
  }
  return "collection_failed";
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

/** 라이브러리 파서의 native 오류는 외부 응답 때문에 발생할 수 있으므로 호출 경계에서 출처를 보존합니다.
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

/** 라이브러리 getter가 외부 응답을 해석하다 실패한 경우에도 동일한 출처 규칙을 적용합니다.
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

/** @param {unknown} error @param {unknown} parserFailure */
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
