// @ts-check

/**
 * @template Request
 * @template Response
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").RpcEndpoint<Request, Response>} endpoint
 * @param {(request: Request) => Promise<unknown>} run
 * @returns {Promise<{ status: number, body: Response | { error: string, error_code: string } }>}
 */
export async function handleRpcRequest(rawBody, endpoint, run) {
  const parsed = parseRpcRequest(rawBody, endpoint.validateRequest);
  if (parsed.ok === false) {
    return { status: 400, body: { error: parsed.error, error_code: "collection_failed" } };
  }
  try {
    return { status: 200, body: endpoint.validateResponse(await run(parsed.value)) };
  } catch (error) {
    return { status: 500, body: rpcErrorBody(error) };
  }
}

export class RpcRequestError extends Error {}

export class RpcResponseError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.code = "parser_drift";
  }
}

/** @param {unknown} error */
export function rpcErrorBody(error) {
  const code = isRecord(error) && typeof error.code === "string" ? error.code : "collection_failed";
  return { error: errorMessage(error), error_code: code };
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
  return {
    channel_id: requiredString(record, "channel_id"),
    ...optionalPositiveIntegers(record, ["max_results", "max_pages", "max_aggregate_bytes"]),
    ...optionalString(record, "proxy_url"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentRequest} */
export function validateContentRequest(value) {
  const request = validateCommunityRequest(value);
  const record = requestRecord(value);
  const kind = requiredString(record, "kind");
  if (kind !== "videos" && kind !== "shorts") {
    throw new RpcRequestError("kind must be videos or shorts");
  }
  return { ...request, kind };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelRequest} */
export function validateChannelRequest(value) {
  const record = requestRecord(value);
  return {
    channel_id: requiredString(record, "channel_id"),
    ...optionalPositiveIntegers(record, ["max_pages", "max_aggregate_bytes"]),
    ...optionalString(record, "proxy_url"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ViewerRequest} */
export function validateViewerRequest(value) {
  const record = requestRecord(value);
  return {
    video_id: requiredString(record, "video_id"),
    ...optionalPositiveIntegers(record, ["max_aggregate_bytes"]),
    ...optionalString(record, "proxy_url"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityResult} */
export function validateCommunityResponse(value) {
  const record = responseRecord(value);
  return {
    posts: arrayField(record, "posts").map(validateCommunityPost),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
    ...optionalResponseString(record, "error"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentResult} */
export function validateContentResponse(value) {
  const record = responseRecord(value);
  return {
    items: arrayField(record, "items").map(validateContentItem),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
    ...optionalResponseString(record, "error"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelResult} */
export function validateChannelResponse(value) {
  const record = responseRecord(value);
  const stats = recordField(record, "stats");
  const profile = recordField(record, "profile");
  return {
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
    ...optionalResponseString(record, "error"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ViewerResult} */
export function validateViewerResponse(value) {
  const record = responseRecord(value);
  return {
    video_id: nonemptyStringField(record, "video_id"),
    ...optionalNullableNonnegativeInteger(record, "viewer_count"),
    availability: validateViewerAvailability(record),
    ...validatePagination(record),
    ...optionalResponseString(record, "error"),
  };
}

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").CommunityRequest, import("./contracts.d.ts").CommunityResult>} */
export const communityEndpoint = {
  validateRequest: validateCommunityRequest,
  validateResponse: validateCommunityResponse,
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ContentRequest, import("./contracts.d.ts").ContentResult>} */
export const contentEndpoint = {
  validateRequest: validateContentRequest,
  validateResponse: validateContentResponse,
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ChannelRequest, import("./contracts.d.ts").ChannelResult>} */
export const channelEndpoint = {
  validateRequest: validateChannelRequest,
  validateResponse: validateChannelResponse,
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ViewerRequest, import("./contracts.d.ts").ViewerResult>} */
export const viewerEndpoint = {
  validateRequest: validateViewerRequest,
  validateResponse: validateViewerResponse,
};

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityPost} */
function validateCommunityPost(value) {
  const record = responseRecord(value);
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
  return {
    url: nonemptyStringField(record, "url"),
    width: nonnegativeIntegerField(record, "width"),
    height: nonnegativeIntegerField(record, "height"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentItem} */
function validateContentItem(value) {
  const record = responseRecord(value);
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    title: stringField(record, "title"),
    ...optionalRFC3339(record, "published_at"),
    ...optionalRFC3339(record, "scheduled_for"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").LiveSessionItem} */
function validateLiveSession(value) {
  const record = responseRecord(value);
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
    result[field] = Number(value);
  }
  return result;
}

/** @param {Record<string, unknown>} record @param {string} field */
function optionalString(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (typeof value !== "string") {
    throw new RpcRequestError(`${field} must be a string`);
  }
  return { [field]: value.trim() };
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").Pagination} */
function validatePagination(record) {
  const pageCount = nonnegativeIntegerField(record, "page_count");
  if (typeof record.exhausted !== "boolean") throw new RpcResponseError("exhausted must be boolean");
  return {
    page_count: pageCount,
    ...optionalResponseString(record, "cursor_start"),
    ...optionalResponseString(record, "cursor_end"),
    exhausted: record.exhausted,
    continuity: validateContinuity(record),
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
function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}
