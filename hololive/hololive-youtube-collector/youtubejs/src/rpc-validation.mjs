// @ts-check

import { encodedSize, maxCursorJSONBytes } from "./pagination.mjs";
import { currentRequestSignal } from "./request-context.mjs";
import { encodeResponseBody } from "./response-encoding.mjs";

/**
 * @template Request
 * @template Response
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").RpcEndpoint<Request, Response>} endpoint
 * @param {(request: Request) => Promise<unknown>} run
 * @param {number} [maximumSuccessResponseBytes]
 * @returns {Promise<{ status: number, body: Response | import("./contracts.d.ts").RPCErrorBody }>}
 */
export async function handleRpcRequest(rawBody, endpoint, run, maximumSuccessResponseBytes = Number.MAX_SAFE_INTEGER) {
  const parsed = parseRpcRequest(rawBody, endpoint.validateRequest);
  if (parsed.ok === false) {
    return rpcErrorResult(400, "invalid_request", "PROTOCOL", parsed.error);
  }
  const requestedLimit = Number(
    /** @type {{ max_success_response_bytes: number }} */ (parsed.value).max_success_response_bytes,
  );
  if (requestedLimit > maximumSuccessResponseBytes) {
    return rpcErrorResult(400, "invalid_request", "PROTOCOL", "max_success_response_bytes exceeds bootstrap limit");
  }
  if (requestedLimit < endpoint.minimumSuccessResponseBytes) {
    return rpcErrorResult(422, "response_too_large", "RESOURCE_LIMIT", "success response metadata exceeds requested limit");
  }
  try {
    const raw = await run(parsed.value);
    if (!isRecord(raw)) {
      throw new RpcResponseError("response must be a JSON object");
    }
    const body = endpoint.validateResponse({ protocol_version: 1, ...raw });
    const encoded = Buffer.byteLength(encodeResponseBody(body));
    if (encoded > requestedLimit) {
      return rpcErrorResult(422, "response_too_large", "RESOURCE_LIMIT", "success response exceeds requested limit");
    }
    return { status: 200, body };
  } catch (error) {
    return rpcErrorResultFor(error);
  }
}

export class RpcRequestError extends Error {}

export class RpcResponseError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "RpcResponseError";
    this.code = "parser_drift";
    this.failureClass = "DATA_CONTRACT";
  }
}

export class RpcProtocolError extends Error {
  constructor(message = "") {
    super(message);
    this.name = "RpcProtocolError";
    this.code = "helper_protocol_mismatch";
    this.failureClass = "PROTOCOL";
  }
}

/** @param {unknown} error */
export function rpcErrorBody(error) {
  return rpcErrorResultFor(error).body;
}

const failureTuples = Object.freeze({
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

/** @param {unknown} error */
export function rpcErrorResultFor(error) {
  if (isCanceledError(error)) {
    return rpcErrorResult(408, "collection_canceled", "CANCELED", "collection canceled");
  }
  if (error instanceof RpcResponseError) {
    return rpcErrorResult(422, "parser_drift", "DATA_CONTRACT", error.message);
  }
  if (isRecord(error) && (error.status === 401 || error.status === 403)) {
    return rpcErrorResult(502, "configuration_error", "CONFIGURATION", safeErrorMessage(error));
  }
  if (isRecord(error) && error.status === 429) {
    const result = rpcErrorResult(429, "cooldown", "COOLDOWN", safeErrorMessage(error));
    if (isRecord(error.retry)) {
      const retry = error.retry;
      if (retry.kind === "after" && Number.isSafeInteger(retry.after_ms) && Number(retry.after_ms) > 0) {
        Object.assign(result.body.error.retry, { kind: "after", after_ms: Number(retry.after_ms) });
      } else if (
        retry.kind === "at" &&
        typeof retry.at === "string" &&
        Number.isFinite(Date.parse(retry.at))
      ) {
        Object.assign(result.body.error.retry, { kind: "at", at: retry.at });
      } else if (retry.kind !== "default") {
        return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", "upstream retry hint is invalid");
      }
    }
    return result;
  }
  if (isRecord(error) && typeof error.code === "string" && Object.hasOwn(failureTuples, error.code)) {
    const code = /** @type {import("./contracts.d.ts").RPCErrorCode} */ (error.code);
    const tuple = failureTuples[code];
    const result = rpcErrorResult(tuple.statuses[0], code, tuple.class, safeErrorMessage(error));
    if ((code === "cooldown" || code === "helper_busy") && isRecord(error.retry)) {
      const retry = error.retry;
      if (retry.kind === "after" && Number.isSafeInteger(retry.after_ms) && Number(retry.after_ms) > 0) {
        Object.assign(result.body.error.retry, { kind: "after", after_ms: Number(retry.after_ms) });
      } else if (
        code === "cooldown" &&
        retry.kind === "at" &&
        typeof retry.at === "string" &&
        Number.isFinite(Date.parse(retry.at))
      ) {
        Object.assign(result.body.error.retry, { kind: "at", at: retry.at });
      } else if (retry.kind !== "default") {
        return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", "upstream retry hint is invalid");
      }
    }
    return result;
  }
  return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", safeErrorMessage(error));
}

/**
 * @param {number} status
 * @param {import("./contracts.d.ts").RPCErrorCode} code
 * @param {import("./contracts.d.ts").RPCFailureClass} failureClass
 * @param {string} message
 */
export function rpcErrorResult(status, code, failureClass, message) {
  const tuple = failureTuples[code];
  if (tuple == null || tuple.class !== failureClass || !tuple.statuses.includes(status)) {
    throw new Error("invalid RPC failure tuple");
  }
  return {
    status,
    body: {
      protocol_version: 1,
      error: {
        code,
        class: failureClass,
        retry: { kind: /** @type {const} */ ("default") },
        message: boundedMessage(message),
      },
    },
  };
}

/**
 * @template T
 * @param {string} rawBody
 * @param {(value: unknown) => T} validate
 * @returns {{ ok: true, value: T } | { ok: false, error: string }}
 */
export function parseRpcRequest(rawBody, validate) {
  /** @type {unknown} */
  let value;
  try {
    value = /** @type {unknown} */ (JSON.parse(rawBody || "{}"));
  } catch {
    return { ok: false, error: "request is not JSON" };
  }
  try {
    return { ok: true, value: validate(value) };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityRequest} */
export function validateCommunityRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, ["protocol_version", "channel_id", "max_success_response_bytes"], ["max_results", "max_pages"]);
  return {
    protocol_version: protocolVersion(record),
    channel_id: requiredString(record, "channel_id"),
    ...optionalPositiveIntegers(record, ["max_results", "max_pages"]),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentRequest} */
export function validateContentRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(
    record,
    ["protocol_version", "channel_id", "kind", "max_success_response_bytes"],
    ["max_results", "max_pages"],
  );
  const kind = requiredString(record, "kind");
  if (kind !== "videos" && kind !== "shorts") {
    throw new RpcRequestError("kind must be videos or shorts");
  }
  return {
    protocol_version: protocolVersion(record),
    channel_id: requiredString(record, "channel_id"),
    kind,
    ...optionalPositiveIntegers(record, ["max_results", "max_pages"]),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelRequest} */
export function validateChannelRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, ["protocol_version", "channel_id", "max_success_response_bytes"], ["max_pages"]);
  return {
    protocol_version: protocolVersion(record),
    channel_id: requiredString(record, "channel_id"),
    ...optionalPositiveIntegers(record, ["max_pages"]),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ViewerRequest} */
export function validateViewerRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, ["protocol_version", "video_id", "max_success_response_bytes"], []);
  return {
    protocol_version: protocolVersion(record),
    video_id: requiredString(record, "video_id"),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityResult} */
export function validateCommunityResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "posts", "page_count", "exhausted", "continuity", "termination_reason"],
    ["cursor_start", "cursor_end", "missing_tab"],
  );
  return {
    protocol_version: responseProtocolVersion(record),
    posts: arrayField(record, "posts").map(validateCommunityPost),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentResult} */
export function validateContentResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "items", "page_count", "exhausted", "continuity", "termination_reason"],
    ["cursor_start", "cursor_end", "missing_tab"],
  );
  return {
    protocol_version: responseProtocolVersion(record),
    items: arrayField(record, "items").map(validateContentItem),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelResult} */
export function validateChannelResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "live_sessions", "stats", "profile", "photo", "page_count", "exhausted", "continuity", "termination_reason"],
    ["cursor_start", "cursor_end", "missing_tab"],
  );
  const stats = recordField(record, "stats");
  const profile = recordField(record, "profile");
  assertResponseKeys(stats, [], ["subscriber_count", "view_count", "video_count"]);
  assertResponseKeys(profile, [], ["handle", "description", "country", "joined_date"]);
  return {
    protocol_version: responseProtocolVersion(record),
    live_sessions: arrayField(record, "live_sessions").map(validateLiveSession),
    stats: {
      ...optionalNullableNonnegativeInteger(stats, "subscriber_count"),
      ...optionalNullableNonnegativeInteger(stats, "view_count"),
      ...optionalNullableNonnegativeInteger(stats, "video_count"),
    },
    profile: {
      ...optionalNullableString(profile, "handle"),
      ...optionalNullableString(profile, "description"),
      ...optionalNullableString(profile, "country"),
      ...optionalNullableString(profile, "joined_date"),
    },
    photo: arrayField(record, "photo").map(validatePhoto),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ViewerResult} */
export function validateViewerResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "video_id", "availability", "page_count", "exhausted", "continuity", "termination_reason"],
    ["viewer_count", "cursor_start", "cursor_end"],
  );
  return {
    protocol_version: responseProtocolVersion(record),
    video_id: nonemptyStringField(record, "video_id"),
    ...optionalNullableNonnegativeInteger(record, "viewer_count"),
    availability: validateViewerAvailability(record),
    ...validatePagination(record),
  };
}

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").CommunityRequest, import("./contracts.d.ts").CommunityResult>} */
export const communityEndpoint = {
  validateRequest: validateCommunityRequest,
  validateResponse: validateCommunityResponse,
  minimumSuccessResponseBytes: Buffer.byteLength(
    JSON.stringify({
      protocol_version: 1,
      posts: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
      termination_reason: "exhausted",
    }),
  ),
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ContentRequest, import("./contracts.d.ts").ContentResult>} */
export const contentEndpoint = {
  validateRequest: validateContentRequest,
  validateResponse: validateContentResponse,
  minimumSuccessResponseBytes: Buffer.byteLength(
    JSON.stringify({
      protocol_version: 1,
      items: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
      termination_reason: "exhausted",
    }),
  ),
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ChannelRequest, import("./contracts.d.ts").ChannelResult>} */
export const channelEndpoint = {
  validateRequest: validateChannelRequest,
  validateResponse: validateChannelResponse,
  minimumSuccessResponseBytes: Buffer.byteLength(JSON.stringify({
    protocol_version: 1,
    live_sessions: [],
    stats: {},
    profile: {},
    photo: [],
    page_count: 1,
    exhausted: true,
    continuity: "NOT_APPLICABLE",
    termination_reason: "exhausted",
  })),
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ViewerRequest, import("./contracts.d.ts").ViewerResult>} */
export const viewerEndpoint = {
  validateRequest: validateViewerRequest,
  validateResponse: validateViewerResponse,
  minimumSuccessResponseBytes: Buffer.byteLength(JSON.stringify({
    protocol_version: 1,
    video_id: "x",
    availability: "UNAVAILABLE",
    page_count: 1,
    exhausted: true,
    continuity: "NOT_APPLICABLE",
    termination_reason: "exhausted",
  })),
};

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityPost} */
function validateCommunityPost(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["postId", "authorId", "authorName", "authorPhoto", "contentText", "publishedText", "likeCount", "commentCount"],
    ["upstreamPostId", "publishedAt", "images", "videoId"],
  );
  return {
    postId: nonemptyStringField(record, "postId"),
    ...optionalResponseString(record, "upstreamPostId"),
    authorId: stringField(record, "authorId"),
    authorName: stringField(record, "authorName"),
    authorPhoto: arrayField(record, "authorPhoto").map(validateThumbnail),
    contentText: stringField(record, "contentText"),
    publishedText: stringField(record, "publishedText"),
    ...optionalRFC3339(record, "publishedAt"),
    likeCount: nonnegativeIntegerField(record, "likeCount"),
    commentCount: nonnegativeIntegerField(record, "commentCount"),
    ...optionalArray(record, "images", validateThumbnail),
    ...optionalResponseString(record, "videoId"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").Thumbnail} */
function validateThumbnail(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, ["url", "width", "height"], []);
  return {
    url: nonemptyStringField(record, "url"),
    width: nonnegativeIntegerField(record, "width"),
    height: nonnegativeIntegerField(record, "height"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentItem} */
function validateContentItem(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, ["video_id", "channel_id", "title"], ["published_at", "scheduled_for", "is_premiere"]);
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    title: stringField(record, "title"),
    ...optionalRFC3339(record, "published_at"),
    ...optionalRFC3339(record, "scheduled_for"),
    ...optionalBoolean(record, "is_premiere"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").LiveSessionItem} */
function validateLiveSession(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, ["video_id", "channel_id", "status"], ["scheduled_at", "started_at", "ended_at"]);
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    status: validateLiveStatus(record),
    ...optionalRFC3339(record, "scheduled_at"),
    ...optionalRFC3339(record, "started_at"),
    ...optionalRFC3339(record, "ended_at"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelPhotoVariant} */
function validatePhoto(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, ["kind", "url", "width", "height"], []);
  return {
    kind: validatePhotoKind(record),
    url: nonemptyStringField(record, "url"),
    width: nonnegativeIntegerField(record, "width"),
    height: nonnegativeIntegerField(record, "height"),
  };
}

/** @param {unknown} value @returns {Record<string, unknown>} */
function requestRecord(value) {
  if (!isRecord(value)) {
    throw new RpcRequestError("request must be a JSON object");
  }
  return value;
}

/**
 * @param {Record<string, unknown>} record
 * @param {string[]} required
 * @param {string[]} optional
 */
function assertRequestKeys(record, required, optional) {
  assertExactKeys(record, required, optional, RpcRequestError);
}

/**
 * @param {Record<string, unknown>} record
 * @param {string[]} required
 * @param {string[]} optional
 */
function assertResponseKeys(record, required, optional) {
  assertExactKeys(record, required, optional, RpcResponseError);
}

/**
 * @param {Record<string, unknown>} record
 * @param {string[]} required
 * @param {string[]} optional
 * @param {typeof RpcRequestError | typeof RpcResponseError} ErrorType
 */
export function assertExactKeys(record, required, optional, ErrorType = RpcRequestError) {
  const allowed = new Set([...required, ...optional]);
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      throw new ErrorType(`unknown field: ${key}`);
    }
  }
  for (const key of required) {
    if (!Object.hasOwn(record, key)) {
      throw new ErrorType(`${key} is required`);
    }
  }
}

/** @param {Record<string, unknown>} record */
function protocolVersion(record) {
  if (record.protocol_version !== 1) {
    throw new RpcRequestError("protocol_version must be 1");
  }
  return 1;
}

/** @param {Record<string, unknown>} record */
function responseProtocolVersion(record) {
  if (record.protocol_version !== 1) {
    throw new RpcResponseError("protocol_version must be 1");
  }
  return 1;
}

/** @param {Record<string, unknown>} record @param {string} field */
function positiveInteger(record, field) {
  const value = record[field];
  if (!Number.isSafeInteger(value) || Number(value) <= 0) {
    throw new RpcRequestError(`${field} must be a positive integer`);
  }
  return Number(value);
}

/** @param {unknown} value @returns {Record<string, unknown>} */
function responseRecord(value) {
  if (!isRecord(value)) {
    throw new RpcResponseError("response must be a JSON object");
  }
  return value;
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** @param {Record<string, unknown>} record @param {string} field */
function requiredString(record, field) {
  const value = record[field];
  if (typeof value !== "string" || value.trim() === "") {
    throw new RpcRequestError(`${field} is required`);
  }
  return value.trim();
}

/** @param {Record<string, unknown>} record @param {string[]} fields */
function optionalPositiveIntegers(record, fields) {
  /** @type {Record<string, number>} */
  const result = {};
  for (const field of fields) {
    const value = record[field];
    if (value === undefined) continue;
    if (!Number.isSafeInteger(value) || Number(value) <= 0) {
      throw new RpcRequestError(`${field} must be a positive integer`);
    }
    if (field === "max_pages" && Number(value) > 100) {
      throw new RpcRequestError("max_pages must not exceed 100");
    }
    if (field === "max_results" && Number(value) > 10_000) {
      throw new RpcRequestError("max_results must not exceed 10000");
    }
    result[field] = Number(value);
  }
  return result;
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").Pagination} */
function validatePagination(record) {
  const pageCount = nonnegativeIntegerField(record, "page_count");
  if (pageCount < 1 || pageCount > 100) {
    throw new RpcProtocolError("page_count is outside the pagination contract");
  }
  if (typeof record.exhausted !== "boolean") throw new RpcResponseError("exhausted must be boolean");
  const continuity = validateContinuity(record);
  const terminationReason = nonemptyStringField(record, "termination_reason");
  if (
    terminationReason !== "exhausted" &&
    terminationReason !== "max_pages" &&
    terminationReason !== "max_results" &&
    terminationReason !== "max_success_response_bytes" &&
    terminationReason !== "cursor_loop" &&
    terminationReason !== "continuation_transient"
  ) {
    throw new RpcProtocolError("termination_reason is invalid");
  }
  if ((terminationReason === "exhausted") !== record.exhausted) {
    throw new RpcProtocolError("termination_reason and exhausted are inconsistent");
  }
  if (
    (terminationReason === "cursor_loop" || terminationReason === "continuation_transient") &&
    continuity !== "GAP_UNRESOLVED"
  ) {
    throw new RpcProtocolError("termination_reason and continuity are inconsistent");
  }
  if (terminationReason === "exhausted" && continuity === "GAP_UNRESOLVED") {
    throw new RpcProtocolError("exhausted pagination cannot have unresolved continuity");
  }
  if (terminationReason !== "exhausted" && continuity === "CONTIGUOUS") {
    throw new RpcProtocolError("partial pagination cannot be contiguous");
  }
  const cursors = {
    ...optionalResponseString(record, "cursor_start"),
    ...optionalResponseString(record, "cursor_end"),
  };
  if (cursors.cursor_start != null && encodedSize(cursors.cursor_start) > maxCursorJSONBytes) {
    throw new RpcProtocolError("pagination cursor exceeds the protocol limit");
  }
  if (cursors.cursor_end != null && encodedSize(cursors.cursor_end) > maxCursorJSONBytes) {
    throw new RpcProtocolError("pagination cursor exceeds the protocol limit");
  }
  return {
    page_count: pageCount,
    ...cursors,
    exhausted: record.exhausted,
    continuity,
    termination_reason: terminationReason,
  };
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").Continuity} */
function validateContinuity(record) {
  const value = nonemptyStringField(record, "continuity");
  if (value === "CONTIGUOUS" || value === "GAP_UNRESOLVED" || value === "NOT_APPLICABLE") {
    return value;
  }
  throw new RpcResponseError("continuity is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").ViewerAvailability} */
function validateViewerAvailability(record) {
  const value = nonemptyStringField(record, "availability");
  if (value === "AVAILABLE" || value === "HIDDEN" || value === "UNAVAILABLE") {
    return value;
  }
  throw new RpcResponseError("viewer availability is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").LiveStatus} */
function validateLiveStatus(record) {
  const value = nonemptyStringField(record, "status");
  if (value === "LIVE" || value === "UPCOMING" || value === "ENDED" || value === "CANCELLED") {
    return value;
  }
  throw new RpcResponseError("live session status is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").PhotoKind} */
function validatePhotoKind(record) {
  const value = nonemptyStringField(record, "kind");
  if (value === "avatar" || value === "banner") {
    return value;
  }
  throw new RpcResponseError("photo kind is invalid");
}

/** @param {Record<string, unknown>} record @param {string} field */
function arrayField(record, field) {
  const value = record[field];
  if (!Array.isArray(value)) throw new RpcResponseError(`${field} must be an array`);
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
function recordField(record, field) {
  const value = record[field];
  if (!isRecord(value)) throw new RpcResponseError(`${field} must be an object`);
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
function nonemptyStringField(record, field) {
  const value = record[field];
  if (typeof value !== "string" || value.trim() === "") {
    throw new RpcResponseError(`${field} must be a non-empty string`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
function stringField(record, field) {
  const value = record[field];
  if (typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalResponseString(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalNullableString(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (value !== null && typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string or null`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalBoolean(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
function nonnegativeIntegerField(record, field) {
  const value = record[field];
  if (!Number.isSafeInteger(value) || Number(value) < 0) {
    throw new RpcResponseError(`${field} must be a non-negative integer`);
  }
  return Number(value);
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalNullableNonnegativeInteger(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (value === null) return { [field]: null };
  return { [field]: nonnegativeIntegerField(record, field) };
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalRFC3339(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  const parsed = typeof value === "string" ? Date.parse(value) : Number.NaN;
  if (!Number.isFinite(parsed) || new Date(parsed).toISOString() !== value) {
    throw new RpcResponseError(`${field} must be an RFC3339 timestamp`);
  }
  return { [field]: value };
}

/**
 * @template T
 * @param {Record<string, unknown>} record
 * @param {string} field
 * @param {(value: unknown) => T} validate
 */
function optionalArray(record, field, validate) {
  const value = record[field];
  if (value === undefined) return {};
  if (!Array.isArray(value)) throw new RpcResponseError(`${field} must be an array`);
  return { [field]: value.map(validate) };
}

/** @param {unknown} error */
function safeErrorMessage(error) {
  const raw = error instanceof Error ? error.message : "helper error";
  return boundedMessage(raw);
}

/** @param {unknown} error */
function errorMessage(error) {
  return error instanceof Error ? boundedMessage(error.message) : "request validation failed";
}

/** @param {string} message */
function boundedMessage(message) {
  const redacted = message
    .replace(/\/\/[^/\s@]+@/g, "//redacted@")
    .replace(/(authorization|x-apikey|cookie)\s*[:=]\s*\S+/gi, "$1=[redacted]")
    .replace(/([?&](?:token|key|secret|password)=)[^&\s]+/gi, "$1[redacted]");
  const raw = Buffer.from(redacted, "utf8");
  if (raw.length <= 512) {
    return redacted;
  }
  let end = 512;
  while (end > 0 && (raw[end] & 0xc0) === 0x80) {
    end -= 1;
  }
  return raw.subarray(0, end).toString("utf8");
}

/** @param {unknown} error */
function isCanceledError(error) {
  if (currentRequestSignal()?.aborted) {
    return true;
  }
  return isRecord(error) && error.code === "collection_canceled";
}
