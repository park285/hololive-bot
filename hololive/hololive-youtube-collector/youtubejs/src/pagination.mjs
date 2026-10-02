import { Mixins, YTNodes } from "youtubei.js";
import { classifyUpstreamError, readUpstream } from "./upstream-errors.mjs";
import { currentRequestSignal } from "./request-context.mjs";

export const continuityContiguous = "CONTIGUOUS";
export const continuityGap = "GAP_UNRESOLVED";
export const continuityNotApplicable = "NOT_APPLICABLE";

const maxPagesLimit = 100;
const maxResultsLimit = 10_000;
const terminationReasons = new Set([
  "exhausted",
  "max_pages",
  "max_results",
  "max_success_response_bytes",
  "cursor_loop",
  "continuation_transient",
]);

export function continuationToken(feed) {
  if (feed == null) {
    return "";
  }
  const token = readUpstream(() => {
    const explicitToken = feed.continuation ?? feed.continuation_token ?? feed.continuationToken;
    if (explicitToken != null) return explicitToken;
    if (feed instanceof Mixins.Feed && feed.has_continuation) {
      const headerContinuations = feed.page.header_memo?.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView) ?? [];
      // about 조회에 쓰는 header continuation은 목록의 이어보기 토큰이 아닙니다.
      const bodyContinuation = feed.memo.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView)
        .find((continuation) => !headerContinuations.includes(continuation));
      return bodyContinuation?.endpoint.payload.token ?? "";
    }
    return "";
  });
  if (typeof token === "string") {
    return token.trim();
  }
  if (typeof token?.token === "string") {
    return token.token.trim();
  }
  return "";
}

export function hasContinuation(feed) {
  if (feed == null) {
    return false;
  }
  if (readUpstream(() => feed.has_continuation === true || feed.hasContinuation === true)) {
    return true;
  }
  return continuationToken(feed) !== "";
}

export function encodedSize(value) {
  return Buffer.byteLength(JSON.stringify(value), "utf8");
}

export class EncodedArrayBudget {
  #limitBytes;
  #reservedEnvelopeBytes;
  #values = [];
  #encodedItemsBytes = 0;

  constructor(limitBytes, reservedEnvelopeBytes) {
    this.#limitBytes = positiveSafeInteger(limitBytes, "max_success_response_bytes");
    this.#reservedEnvelopeBytes = nonnegativeSafeInteger(reservedEnvelopeBytes, "reserved envelope bytes");
  }

  tryAppend(value) {
    let encodedBytes;
    try {
      encodedBytes = encodedSize(value);
    } catch {
      throw codedError("helper_internal_invariant", "pagination item is not JSON serializable");
    }
    const commaBytes = this.#values.length === 0 ? 0 : 1;
    if (this.#reservedEnvelopeBytes + this.#encodedItemsBytes + commaBytes + encodedBytes > this.#limitBytes) {
      return "WOULD_EXCEED";
    }
    this.#values.push(value);
    this.#encodedItemsBytes += commaBytes + encodedBytes;
    return "APPENDED";
  }

  values() {
    return [...this.#values];
  }

  encodedItemsBytes() {
    return this.#encodedItemsBytes;
  }

  count() {
    return this.#values.length;
  }
}

export function paginationEnvelopeReserve(skeleton) {
  return encodedSize({
    ...skeleton,
    page_count: maxPagesLimit,
    exhausted: false,
    continuity: continuityGap,
    termination_reason: "max_success_response_bytes",
  });
}

export function assertResponseBudget(limitBytes, reservedEnvelopeBytes) {
  const limit = positiveSafeInteger(limitBytes, "max_success_response_bytes");
  const reserve = nonnegativeSafeInteger(reservedEnvelopeBytes, "reserved envelope bytes");
  if (reserve >= limit) {
    throw codedError(
      "response_too_large",
      "success response metadata exceeds requested limit",
    );
  }
}

export function paginationResult({
  pageCount,
  reason,
  continuity,
}) {
  if (!Number.isSafeInteger(pageCount) || pageCount < 1 || pageCount > maxPagesLimit) {
    throw protocolFault("page count is outside the pagination contract");
  }
  if (!terminationReasons.has(reason)) {
    throw protocolFault("termination reason is outside the pagination contract");
  }
  let resolvedContinuity = continuity;
  if (reason === "cursor_loop" || reason === "continuation_transient") {
    if (continuity != null && continuity !== continuityGap) {
      throw protocolFault("termination reason and continuity are inconsistent");
    }
    resolvedContinuity = continuityGap;
  } else if (resolvedContinuity == null) {
    resolvedContinuity = reason === "exhausted" ? continuityContiguous : continuityGap;
  }
  if (
    resolvedContinuity !== continuityContiguous &&
    resolvedContinuity !== continuityGap &&
    resolvedContinuity !== continuityNotApplicable
  ) {
    throw protocolFault("continuity is outside the pagination contract");
  }
  if (reason === "exhausted" && resolvedContinuity === continuityGap) {
    throw protocolFault("exhausted pagination cannot have unresolved continuity");
  }
  if (reason !== "exhausted" && resolvedContinuity === continuityContiguous) {
    throw protocolFault("partial pagination cannot be contiguous");
  }
  return {
    page_count: pageCount,
    exhausted: reason === "exhausted",
    continuity: resolvedContinuity,
    termination_reason: reason,
  };
}

export async function paginate({
  firstPage,
  getContinuation,
  mapPage,
  maxPages,
  maxResults,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  reservedEnvelopeBytes,
  buildResult = (items, pagination) => ({ items, ...pagination }),
}) {
  const pages = boundedInteger(maxPages, 1, maxPagesLimit, 1, "max_pages");
  const resultLimit = boundedInteger(maxResults, 1, maxResultsLimit, maxResultsLimit, "max_results");
  const reserve = nonnegativeSafeInteger(reservedEnvelopeBytes, "reserved envelope bytes");
  assertResponseBudget(maxSuccessResponseBytes, reserve);
  const budget = new EncodedArrayBudget(maxSuccessResponseBytes, reserve);
  // continuation token은 반복 감지에만 씁니다. 같은 목록이라도 요청마다 달라질 수 있고 수 KB까지
  // 커지므로 응답에 실으면 observation scope가 흔들리고 coverage 계약의 길이 상한을 넘습니다.
  const seen = new Set();
  let pageCount = 0;
  let feed = firstPage;
  let reason = "";
  while (feed != null) {
    assertParentRequestAlive();
    const mapped = await mapPage(feed);
    if (Array.isArray(mapped) || mapped == null || mapped.recognized_shape !== true || !isIterable(mapped.items)) {
      throw codedError(
        "helper_internal_invariant",
        "helper page mapper violated the recognized page contract",
      );
    }
    pageCount += 1;
    const cursor = continuationToken(feed);
    for await (const item of mapped.items) {
      if (budget.tryAppend(item) === "WOULD_EXCEED") {
        if (budget.count() === 0) {
          throw codedError("response_too_large", "first valid item exceeds success response limit");
        }
        reason = "max_success_response_bytes";
        break;
      }
      // 다음 next()가 metadata 조회를 시작할 수 있으므로 채택 직후 멈춥니다.
      if (budget.count() >= resultLimit) {
        reason = "max_results";
        break;
      }
    }
    if (reason !== "") {
      break;
    }
    if (!hasContinuation(feed)) {
      reason = "exhausted";
      break;
    }
    if (cursor !== "" && seen.has(cursor)) {
      reason = "cursor_loop";
      break;
    }
    if (cursor !== "") {
      seen.add(cursor);
    }
    if (pageCount >= pages) {
      reason = "max_pages";
      break;
    }
    if (typeof getContinuation !== "function") {
      throw codedError("parser_drift", "pagination continuation is missing");
    }
    try {
      feed = await getContinuation(feed);
    } catch (error) {
      if (currentRequestSignal()?.aborted) {
        throw error;
      }
      if (isContinuationTransient(error)) {
        reason = "continuation_transient";
        break;
      }
      throw error;
    }
  }
  if (pageCount < 1) {
    throw codedError("parser_drift", "helper produced no validated page");
  }
  if (reason === "") {
    throw codedError("parser_drift", "pagination ended without a termination reason");
  }
  assertParentRequestAlive();
  const pagination = paginationResult({
    pageCount,
    reason,
    continuity: undefined,
  });
  return buildResult(budget.values(), pagination);
}

function isIterable(items) {
  return items != null &&
    (typeof items[Symbol.iterator] === "function" || typeof items[Symbol.asyncIterator] === "function");
}

function assertParentRequestAlive() {
  if (currentRequestSignal()?.aborted) {
    throw new DOMException("aborted", "AbortError");
  }
}

function isContinuationTransient(error) {
  const code = classifyUpstreamError(error);
  return code === "collection_timeout" || code === "collection_failed" || code === "helper_busy";
}

function boundedInteger(value, minimum, maximum, fallback, field) {
  if (value == null) {
    return fallback;
  }
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw protocolFault(`${field} is outside the protocol limit`);
  }
  return value;
}

function positiveSafeInteger(value, field) {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw protocolFault(`${field} must be a positive integer`);
  }
  return value;
}

function nonnegativeSafeInteger(value, field) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw protocolFault(`${field} must be a non-negative integer`);
  }
  return value;
}

function protocolFault(message) {
  return codedError("helper_protocol_mismatch", message);
}

function codedError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}
