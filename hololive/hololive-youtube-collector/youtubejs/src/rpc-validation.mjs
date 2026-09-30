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
  assertRequestKeys(record, ["protocol_version", "channel_id", "kind", "max_success_response_bytes"], ["max_pages"]);
  const kind = requiredString(record, "kind");
  if (kind !== "live" && kind !== "metadata") {
    throw new RpcRequestError("kind must be live or metadata");
  }
  return {
    protocol_version: protocolVersion(record),
    channel_id: requiredString(record, "channel_id"),
    kind,
    ...optionalPositiveIntegers(record, ["max_pages"]),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

const maxChannelIdentifierBytes = 256;
const maxVideoIdentifierBytes = 128;

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelLiveCheckRequest} */
export function validateChannelLiveCheckRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, ["protocol_version", "channel_id", "max_success_response_bytes"], []);
  return {
    protocol_version: protocolVersion(record),
    channel_id: requestIdentifier(record, "channel_id", maxChannelIdentifierBytes),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").VideoLiveCheckRequest} */
export function validateVideoLiveCheckRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, ["protocol_version", "video_id", "max_success_response_bytes"], []);
  return {
    protocol_version: protocolVersion(record),
    video_id: requestIdentifier(record, "video_id", maxVideoIdentifierBytes),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/**
 * 채널 확인 결과는 pagination 없이 자체 불변식으로 검증합니다. 확인된 결과만 identity를 확정하고,
 * CHANNEL_PAGE는 선택 영상이 없으며 LIVE_VIDEO·UPCOMING_VIDEO는 선택 영상이 있어야 합니다.
 * @param {unknown} value
 * @returns {import("./contracts.d.ts").ChannelLiveCheckResult}
 */
export function validateChannelLiveCheckResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "channel_id", "outcome", "channel_identity_confirmed"],
    ["selected_video_id", "unknown_reason"],
  );
  const channelId = responseIdentifier(record, "channel_id", maxChannelIdentifierBytes);
  const outcome = channelLiveCheckOutcome(record);
  const confirmed = requiredResponseBoolean(record, "channel_identity_confirmed");
  const selectedVideoId = Object.hasOwn(record, "selected_video_id")
    ? responseIdentifier(record, "selected_video_id", maxVideoIdentifierBytes)
    : undefined;
  /** @type {import("./contracts.d.ts").ChannelLiveCheckUnknownReason | undefined} */
  let unknownReason;
  if (outcome === "UNKNOWN") {
    unknownReason = channelLiveCheckUnknownReason(record);
    if (confirmed && (unknownReason === "identity_missing" || unknownReason === "identity_mismatch")) {
      throw new RpcResponseError("channel live check identity failure cannot confirm identity");
    }
  } else {
    if (Object.hasOwn(record, "unknown_reason")) {
      throw new RpcResponseError("known channel live check outcome must not carry an unknown reason");
    }
    if (!confirmed) {
      throw new RpcResponseError("known channel live check outcome requires confirmed identity");
    }
    if ((outcome === "CHANNEL_PAGE") !== (selectedVideoId === undefined)) {
      throw new RpcResponseError("channel live check selected video does not match the outcome");
    }
  }
  return {
    protocol_version: responseProtocolVersion(record),
    channel_id: channelId,
    outcome,
    ...(selectedVideoId === undefined ? {} : { selected_video_id: selectedVideoId }),
    channel_identity_confirmed: confirmed,
    ...(unknownReason === undefined ? {} : { unknown_reason: unknownReason }),
  };
}

/**
 * 영상 확인 결과를 검증합니다. method는 availability에서만 결정되고, UNKNOWN일 때만 사유가 있습니다.
 * identity가 확인되지 않은 결과는 다른 영상의 사실을 싣지 않으며, 신뢰 가능한 수명 사실은 서로 모순되지 않아야 합니다.
 * @param {unknown} value
 * @returns {import("./contracts.d.ts").VideoLiveCheckResult}
 */
export function validateVideoLiveCheckResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(
    record,
    ["protocol_version", "video_id", "identity_confirmed", "availability", "method"],
    [
      "channel_id",
      "is_live",
      "is_live_now",
      "is_upcoming",
      "is_live_content",
      "is_private",
      "has_live_broadcast_details",
      "started_at",
      "scheduled_at",
      "waiting_state_confirmed",
      "ended_at",
      "unknown_reason",
    ],
  );
  const videoId = responseIdentifier(record, "video_id", maxVideoIdentifierBytes);
  const channelId = Object.hasOwn(record, "channel_id")
    ? responseIdentifier(record, "channel_id", maxChannelIdentifierBytes)
    : undefined;
  const confirmed = requiredResponseBoolean(record, "identity_confirmed");
  const availability = videoAvailability(record);
  const method = videoAvailabilityMethod(record);
  if (method !== availabilityMethodFor(availability)) {
    throw new RpcResponseError("video live check method does not match availability");
  }
  const facts = {
    is_live: optionalResponseBoolean(record, "is_live"),
    is_live_now: optionalResponseBoolean(record, "is_live_now"),
    is_upcoming: optionalResponseBoolean(record, "is_upcoming"),
    is_live_content: optionalResponseBoolean(record, "is_live_content"),
    is_private: optionalResponseBoolean(record, "is_private"),
    has_live_broadcast_details: optionalResponseBoolean(record, "has_live_broadcast_details"),
    started_at: optionalResponseTimestamp(record, "started_at"),
    scheduled_at: optionalResponseTimestamp(record, "scheduled_at"),
    waiting_state_confirmed: optionalResponseBoolean(record, "waiting_state_confirmed"),
    ended_at: optionalResponseTimestamp(record, "ended_at"),
  };
  if (confirmed && channelId === undefined) {
    throw new RpcResponseError("confirmed video live check identity requires channel_id");
  }
  if (!confirmed && (channelId !== undefined || Object.values(facts).some((fact) => fact !== undefined))) {
    throw new RpcResponseError("unconfirmed video live check must not carry response facts");
  }
  /** @type {import("./contracts.d.ts").VideoLiveCheckUnknownReason | undefined} */
  let unknownReason;
  if (availability === "UNKNOWN") {
    unknownReason = videoLiveCheckUnknownReason(record);
    const identityStage = unknownReason === "identity_missing" || unknownReason === "identity_mismatch";
    if (confirmed && identityStage) {
      throw new RpcResponseError("video live check identity failure cannot confirm identity");
    }
    if (!confirmed && !identityStage && unknownReason !== "structure_unrecognized") {
      throw new RpcResponseError("video live check reason requires confirmed identity");
    }
  } else {
    if (Object.hasOwn(record, "unknown_reason")) {
      throw new RpcResponseError("known video availability must not carry an unknown reason");
    }
    if (!confirmed) {
      throw new RpcResponseError("known video availability requires confirmed identity");
    }
  }
  if (confirmed && (unknownReason === undefined || unknownReason === "availability_unclassified")) {
    assertLifecycleFacts(facts, Date.now());
  }
  if (availability === "PUBLIC" && facts.is_private !== false) {
    throw new RpcResponseError("PUBLIC availability requires raw is_private=false");
  }
  if (availability === "PUBLIC_UNAVAILABLE" && facts.is_private !== true) {
    throw new RpcResponseError("PUBLIC_UNAVAILABLE availability requires raw is_private=true");
  }
  return {
    protocol_version: responseProtocolVersion(record),
    video_id: videoId,
    ...(channelId === undefined ? {} : { channel_id: channelId }),
    identity_confirmed: confirmed,
    ...(facts.is_live === undefined ? {} : { is_live: facts.is_live }),
    ...(facts.is_live_now === undefined ? {} : { is_live_now: facts.is_live_now }),
    ...(facts.is_upcoming === undefined ? {} : { is_upcoming: facts.is_upcoming }),
    ...(facts.is_live_content === undefined ? {} : { is_live_content: facts.is_live_content }),
    ...(facts.is_private === undefined ? {} : { is_private: facts.is_private }),
    ...(facts.has_live_broadcast_details === undefined
      ? {}
      : { has_live_broadcast_details: facts.has_live_broadcast_details }),
    ...(facts.started_at === undefined ? {} : { started_at: facts.started_at }),
    ...(facts.scheduled_at === undefined ? {} : { scheduled_at: facts.scheduled_at }),
    ...(facts.waiting_state_confirmed === undefined ? {} : { waiting_state_confirmed: facts.waiting_state_confirmed }),
    ...(facts.ended_at === undefined ? {} : { ended_at: facts.ended_at }),
    availability,
    method,
    ...(unknownReason === undefined ? {} : { unknown_reason: unknownReason }),
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
    ["protocol_version", "live_sessions", "profile", "photo", "page_count", "exhausted", "continuity", "termination_reason"],
    ["cursor_start", "cursor_end", "missing_tab", "unavailable_live_sessions", "live_query"],
  );
  const profile = recordField(record, "profile");
  assertResponseKeys(profile, [], ["handle", "description", "country", "joined_date"]);
  const liveSessions = arrayField(record, "live_sessions").map(validateLiveSession);
  const unavailable = Object.hasOwn(record, "unavailable_live_sessions")
    ? arrayField(record, "unavailable_live_sessions").map(validateUnavailableLiveSession)
    : undefined;
  if (unavailable != null) {
    if (unavailable.length > 32 || (record.missing_tab === true && unavailable.length > 0)) {
      throw new RpcResponseError("unavailable live sessions are outside the collection scope");
    }
    const seen = new Set(liveSessions.map((item) => item.video_id));
    for (const item of unavailable) {
      if (seen.has(item.video_id)) {
        throw new RpcResponseError("unavailable live session overlaps another row");
      }
      seen.add(item.video_id);
    }
  }
  return {
    protocol_version: responseProtocolVersion(record),
    live_sessions: liveSessions,
    ...(unavailable == null ? {} : { unavailable_live_sessions: unavailable }),
    profile: {
      ...optionalNullableString(profile, "handle"),
      ...optionalNullableString(profile, "description"),
      ...optionalNullableString(profile, "country"),
      ...optionalNullableString(profile, "joined_date"),
    },
    photo: arrayField(record, "photo").map(validatePhoto),
    ...validatePagination(record),
    ...optionalBoolean(record, "missing_tab"),
    ...(record.live_query === undefined ? {} : { live_query: validateLiveQuery(record, unavailable?.length ?? 0) }),
  };
}

/**
 * @param {Record<string, unknown>} parent
 * @param {number} unavailableCount
 * @returns {NonNullable<import("./contracts.d.ts").ChannelResult["live_query"]>}
 */
function validateLiveQuery(parent, unavailableCount) {
  const query = recordField(parent, "live_query");
  assertResponseKeys(query, ["channel_id", "source", "statuses", "exhausted", "access_restricted", "page_count"], []);
  const channelId = responseIdentifier(query, "channel_id", maxChannelIdentifierBytes);
  const statuses = arrayField(query, "statuses");
  if (typeof query.page_count !== "number" || query.source !== "streams" || JSON.stringify(statuses) !== JSON.stringify(["ENDED", "LIVE", "UPCOMING"]) ||
      query.page_count !== parent.page_count || query.exhausted !== parent.exhausted ||
      query.access_restricted !== (unavailableCount > 0) || parent.missing_tab === true) {
    throw new RpcResponseError("live query proof contradicts collection scope");
  }
  return { channel_id: channelId, source: "streams", statuses: ["ENDED", "LIVE", "UPCOMING"],
    exhausted: requiredResponseBoolean(query, "exhausted"), access_restricted: requiredResponseBoolean(query, "access_restricted"),
    page_count: query.page_count };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").UnavailableLiveSession} */
function validateUnavailableLiveSession(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, ["video_id", "channel_id", "reason"], []);
  if (record.reason !== "access_restricted") {
    throw new RpcResponseError("unavailable live session reason is invalid");
  }
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    reason: record.reason,
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
    profile: {},
    photo: [],
    page_count: 1,
    exhausted: true,
    continuity: "NOT_APPLICABLE",
    termination_reason: "exhausted",
  })),
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").ChannelLiveCheckRequest, import("./contracts.d.ts").ChannelLiveCheckResult>} */
export const channelLiveCheckEndpoint = {
  validateRequest: validateChannelLiveCheckRequest,
  validateResponse: validateChannelLiveCheckResponse,
  // 가장 작은 성공 본문은 선택 영상·사유가 없는 CHANNEL_PAGE이며 실제 채널 ID 길이는 성공 후 다시 계산합니다.
  minimumSuccessResponseBytes: Buffer.byteLength(JSON.stringify({
    protocol_version: 1,
    channel_id: "",
    outcome: "CHANNEL_PAGE",
    channel_identity_confirmed: true,
  })),
};

/** @type {import("./contracts.d.ts").RpcEndpoint<import("./contracts.d.ts").VideoLiveCheckRequest, import("./contracts.d.ts").VideoLiveCheckResult>} */
export const videoLiveCheckEndpoint = {
  validateRequest: validateVideoLiveCheckRequest,
  validateResponse: validateVideoLiveCheckResponse,
  // identity 미확인 UNKNOWN과 확인된 MEMBERS_ONLY 중 더 작은 본문이 성공 응답의 하한입니다.
  minimumSuccessResponseBytes: Math.min(
    Buffer.byteLength(JSON.stringify({
      protocol_version: 1,
      video_id: "",
      identity_confirmed: false,
      availability: "UNKNOWN",
      method: "unknown",
      unknown_reason: "identity_missing",
    })),
    Buffer.byteLength(JSON.stringify({
      protocol_version: 1,
      video_id: "",
      channel_id: "",
      identity_confirmed: true,
      availability: "MEMBERS_ONLY",
      method: "player_members_only",
    })),
  ),
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
  assertResponseKeys(
    record,
    ["video_id", "channel_id", "status"],
    ["title", "thumbnail_url", "scheduled_at", "started_at", "ended_at"],
  );
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    status: validateLiveStatus(record),
    ...optionalResponseString(record, "title"),
    ...optionalResponseString(record, "thumbnail_url"),
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
function optionalRFC3339(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  const parsed = typeof value === "string" ? Date.parse(value) : Number.NaN;
  if (!Number.isFinite(parsed) || new Date(parsed).toISOString() !== value) {
    throw new RpcResponseError(`${field} must be an RFC3339 timestamp`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field @returns {string | undefined} */
function optionalResponseTimestamp(record, field) {
  const value = record[field];
  if (value === undefined) return undefined;
  const parsed = typeof value === "string" ? Date.parse(value) : Number.NaN;
  if (typeof value !== "string" || !Number.isFinite(parsed) || new Date(parsed).toISOString() !== value) {
    throw new RpcResponseError(`${field} must be an RFC3339 timestamp`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @returns {boolean | undefined} */
function optionalResponseBoolean(record, field) {
  const value = record[field];
  if (value === undefined) return undefined;
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @returns {boolean} */
function requiredResponseBoolean(record, field) {
  const value = record[field];
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return value;
}

// Go 관측 계약의 식별자 규칙과 같게 빈 값·앞뒤 공백·길이 초과를 거부합니다. 요청 subject를 변형하지 않고 그대로 되돌려야 합니다.
/** @param {unknown} value @param {number} maxBytes */
function validIdentifier(value, maxBytes) {
  return typeof value === "string" && value.trim() !== "" && value.trim() === value && Buffer.byteLength(value) <= maxBytes;
}

/** @param {Record<string, unknown>} record @param {string} field @param {number} maxBytes @returns {string} */
function requestIdentifier(record, field, maxBytes) {
  const value = record[field];
  if (typeof value !== "string" || !validIdentifier(value, maxBytes)) {
    throw new RpcRequestError(`${field} must be a trimmed identifier of at most ${maxBytes} bytes`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @param {number} maxBytes @returns {string} */
function responseIdentifier(record, field, maxBytes) {
  const value = record[field];
  if (typeof value !== "string" || !validIdentifier(value, maxBytes)) {
    throw new RpcResponseError(`${field} must be a trimmed identifier of at most ${maxBytes} bytes`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").ChannelLiveCheckOutcome} */
function channelLiveCheckOutcome(record) {
  const value = record.outcome;
  if (value === "LIVE_VIDEO" || value === "UPCOMING_VIDEO" || value === "CHANNEL_PAGE" || value === "UNKNOWN") {
    return value;
  }
  throw new RpcResponseError("channel live check outcome is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").ChannelLiveCheckUnknownReason} */
function channelLiveCheckUnknownReason(record) {
  const value = record.unknown_reason;
  if (
    value === "identity_missing" ||
    value === "identity_mismatch" ||
    value === "contradictory_fields" ||
    value === "structure_unrecognized" ||
    value === "not_waiting_state" ||
    value === "login_required_unclassified" ||
    value === "error_unclassified"
  ) {
    return value;
  }
  throw new RpcResponseError("channel live check unknown_reason is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").VideoLiveCheckUnknownReason} */
function videoLiveCheckUnknownReason(record) {
  const value = record.unknown_reason;
  if (
    value === "identity_missing" ||
    value === "identity_mismatch" ||
    value === "contradictory_fields" ||
    value === "structure_unrecognized" ||
    value === "login_required_unclassified" ||
    value === "error_unclassified" ||
    value === "availability_unclassified"
  ) {
    return value;
  }
  throw new RpcResponseError("video live check unknown_reason is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").VideoAvailability} */
function videoAvailability(record) {
  const value = record.availability;
  if (value === "PUBLIC" || value === "MEMBERS_ONLY" || value === "PUBLIC_UNAVAILABLE" || value === "UNKNOWN") {
    return value;
  }
  throw new RpcResponseError("video live check availability is invalid");
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").VideoAvailabilityMethod} */
function videoAvailabilityMethod(record) {
  const value = record.method;
  if (value === "player_public" || value === "player_members_only" || value === "player_private" || value === "unknown") {
    return value;
  }
  throw new RpcResponseError("video live check method is invalid");
}

/**
 * @param {import("./contracts.d.ts").VideoAvailability} availability
 * @returns {import("./contracts.d.ts").VideoAvailabilityMethod}
 */
function availabilityMethodFor(availability) {
  switch (availability) {
    case "PUBLIC":
      return "player_public";
    case "MEMBERS_ONLY":
      return "player_members_only";
    case "PUBLIC_UNAVAILABLE":
      return "player_private";
    case "UNKNOWN":
      return "unknown";
  }
}

/**
 * 신뢰 가능한 수명 사실의 모순을 거부합니다. isLive 생략·false와 종료 시각의 조합은 종료 근거입니다.
 * @param {{ is_live?: boolean, is_live_now?: boolean, is_upcoming?: boolean, started_at?: string, ended_at?: string }} facts
 * @param {number} nowMs
 */
function assertLifecycleFacts(facts, nowMs) {
  if (facts.is_live !== undefined && facts.is_live_now !== undefined && facts.is_live !== facts.is_live_now) {
    throw new RpcResponseError("video live check is_live and is_live_now disagree");
  }
  const live = facts.is_live === true || facts.is_live_now === true;
  if (live && (facts.is_upcoming === true || facts.ended_at !== undefined)) {
    throw new RpcResponseError("video live check live fact coexists with upcoming or end facts");
  }
  if (facts.ended_at === undefined) {
    return;
  }
  if (facts.is_upcoming === true) {
    throw new RpcResponseError("video live check upcoming fact coexists with end facts");
  }
  const endedMs = Date.parse(facts.ended_at);
  if (facts.started_at !== undefined && endedMs < Date.parse(facts.started_at)) {
    throw new RpcResponseError("video live check ended_at precedes started_at");
  }
  if (endedMs > nowMs) {
    throw new RpcResponseError("video live check ended_at is in the future");
  }
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
