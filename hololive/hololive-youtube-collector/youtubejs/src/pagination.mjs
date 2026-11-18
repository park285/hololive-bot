import { Mixins, YTNodes } from "youtubei.js";
import { FetchTransportError } from "./fetch-transport.mjs";
import { classifyUpstreamError, isTransientNetworkError, readUpstream, upstreamHTTPStatus } from "./upstream-errors.mjs";
import { currentRequestSignal } from "./request-context.mjs";

export const continuityContiguous = "CONTIGUOUS";
export const continuityGap = "GAP_UNRESOLVED";
export const continuityNotApplicable = "NOT_APPLICABLE";

export const maxPagesLimit = 100;
export const maxResultsLimit = 10_000;
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
    if (feed instanceof Mixins.Feed && feed.has_continuation) {
      const headerContinuations = feed.page.header_memo?.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView) ?? [];
      const bodyContinuation = feed.memo.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView)
        .find((continuation) => !headerContinuations.includes(continuation));
      return bodyContinuation?.endpoint.payload.token ?? "";
    }
    return "";
  });
  return typeof token === "string" ? token.trim() : "";
}

export function hasContinuation(feed) {
  return feed != null && readUpstream(() => feed.has_continuation === true);
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
  return validatedPagination({
    page_count: pageCount,
    exhausted: reason === "exhausted",
    continuity: continuity ?? (reason === "exhausted" ? continuityContiguous : continuityGap),
    termination_reason: reason,
  });
}

export function validatedPagination({ page_count, exhausted, continuity, termination_reason }) {
  if (!Number.isSafeInteger(page_count) || page_count < 1 || page_count > maxPagesLimit) {
    throw invariantFault("page_count is outside the pagination contract");
  }
  if (!terminationReasons.has(termination_reason)) {
    throw invariantFault("termination_reason is outside the pagination contract");
  }
  if (continuity !== continuityContiguous && continuity !== continuityGap && continuity !== continuityNotApplicable) {
    throw invariantFault("continuity is outside the pagination contract");
  }
  if (exhausted !== (termination_reason === "exhausted")) {
    throw invariantFault("termination_reason and exhausted are inconsistent");
  }
  if ((termination_reason === "cursor_loop" || termination_reason === "continuation_transient") && continuity !== continuityGap) {
    throw invariantFault("termination_reason and continuity are inconsistent");
  }
  if (termination_reason === "exhausted" && continuity === continuityGap) {
    throw invariantFault("exhausted pagination cannot have unresolved continuity");
  }
  if (termination_reason !== "exhausted" && continuity === continuityContiguous) {
    throw invariantFault("partial pagination cannot be contiguous");
  }
  return { page_count, exhausted, continuity, termination_reason };
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
  const pages = boundedInteger(maxPages, maxPagesLimit, "max_pages");
  const resultLimit = boundedInteger(maxResults, maxResultsLimit, "max_results");
  const reserve = nonnegativeSafeInteger(reservedEnvelopeBytes, "reserved envelope bytes");
  assertResponseBudget(maxSuccessResponseBytes, reserve);
  const budget = new EncodedArrayBudget(maxSuccessResponseBytes, reserve);
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
      throw continuationFailure(error);
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
  if (code === "collection_timeout") return true;
  if (code !== "collection_failed") return false;
  for (let current = error, depth = 0; current != null && depth < 4; current = current.cause, depth += 1) {
    if ((current instanceof FetchTransportError && current.code === "collection_failed") || isTransientNetworkError(current)) {
      return true;
    }
  }
  return false;
}

function continuationFailure(error) {
  if (classifyUpstreamError(error) !== "collection_failed" || upstreamHTTPStatus(error) !== undefined) {
    return error;
  }
  return Object.assign(new Error("continuation response could not be processed", { cause: error }), { code: "parser_drift" });
}

function boundedInteger(value, maximum, field) {
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) {
    throw invariantFault(`${field} is outside the protocol limit`);
  }
  return value;
}

function positiveSafeInteger(value, field) {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw invariantFault(`${field} must be a positive integer`);
  }
  return value;
}

function nonnegativeSafeInteger(value, field) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw invariantFault(`${field} must be a non-negative integer`);
  }
  return value;
}

function invariantFault(message) {
  return codedError("helper_internal_invariant", message);
}

function codedError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}
