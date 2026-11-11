// @ts-check

import { maxPagesLimit, maxResultsLimit, validatedPagination } from "./pagination.mjs";
import { isCalendarRFC3339 } from "./rfc3339.mjs";
import { RpcRequestError, RpcResponseError } from "./rpc-errors.mjs";
import {
  arrayField,
  assertRequestKeys,
  assertResponseKeys,
  keySchema,
  maxChannelIdentifierBytes,
  maxScheduleMetadataLookups,
  nonemptyStringField,
  nonnegativeIntegerField,
  optionalBoolean,
  optionalNullableString,
  optionalResponseString,
  optionalRFC3339,
  positiveInteger,
  protocolVersion,
  recordField,
  requestIdentifier,
  requestRecord,
  requiredResponseBoolean,
  responseIdentifier,
  responseProtocolVersion,
  responseRecord,
  stringField,
} from "./rpc-validation-primitives.mjs";

const communityRequestKeys = keySchema(["protocol_version", "channel_id", "max_results", "max_pages", "max_success_response_bytes"]);

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityRequest} */
function validateCommunityRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, communityRequestKeys);
  return {
    protocol_version: protocolVersion(record),
    channel_id: requestIdentifier(record, "channel_id", maxChannelIdentifierBytes),
    ...pageLimits(record),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

const contentRequestKeys = keySchema(["protocol_version", "channel_id", "kind", "max_results", "max_pages", "max_success_response_bytes"]);

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentRequest} */
function validateContentRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, contentRequestKeys);
  const kind = record.kind;
  if (kind !== "videos" && kind !== "shorts") {
    throw new RpcRequestError("kind must be videos or shorts");
  }
  return {
    protocol_version: protocolVersion(record),
    channel_id: requestIdentifier(record, "channel_id", maxChannelIdentifierBytes),
    kind,
    ...pageLimits(record),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

const channelRequestKeys = keySchema(
  ["protocol_version", "channel_id", "kind", "max_success_response_bytes"],
  ["max_schedule_lookups", "scheduled_for"],
);

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelRequest} */
function validateChannelRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, channelRequestKeys);
  const kind = record.kind;
  if (kind !== "live" && kind !== "metadata") {
    throw new RpcRequestError("kind must be live or metadata");
  }
  // live 요청은 예정 영상 조회 상한을 반드시 지정하고, metadata 요청은 조회하지 않으므로 지정하지 않습니다.
  if ((kind === "live") !== Object.hasOwn(record, "max_schedule_lookups")) {
    throw new RpcRequestError("max_schedule_lookups must be present only for live requests");
  }
  const lookups = record.max_schedule_lookups;
  if (lookups !== undefined && (!Number.isSafeInteger(lookups) || Number(lookups) < 0 || Number(lookups) > maxScheduleMetadataLookups)) {
    throw new RpcRequestError(`max_schedule_lookups must be an integer between 0 and ${maxScheduleMetadataLookups}`);
  }
  // collector lease 슬롯 시각입니다. 재사용 일정의 원래 관측 시각을 API의 관측 시각과 같은 시계로 남기는 데만 씁니다.
  const scheduledFor = record.scheduled_for;
  if (scheduledFor !== undefined && (kind !== "live" || typeof scheduledFor !== "string" ||
      !isCalendarRFC3339(scheduledFor) || !Number.isFinite(Date.parse(scheduledFor)))) {
    throw new RpcRequestError("scheduled_for must be an RFC3339 timestamp on live requests");
  }
  return {
    protocol_version: protocolVersion(record),
    channel_id: requestIdentifier(record, "channel_id", maxChannelIdentifierBytes),
    kind,
    ...(lookups === undefined ? {} : { max_schedule_lookups: Number(lookups) }),
    ...(scheduledFor === undefined ? {} : { scheduled_for: scheduledFor }),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

const communityResponseKeys = keySchema(
  ["protocol_version", "posts", "page_count", "exhausted", "continuity", "termination_reason"],
  ["missing_tab"],
);
const contentResponseKeys = keySchema(
  ["protocol_version", "items", "page_count", "exhausted", "continuity", "termination_reason"],
  ["missing_tab"],
);
const channelResponseKeys = keySchema(
  ["protocol_version", "live_sessions", "profile", "photo", "page_count", "exhausted", "continuity", "termination_reason"],
  ["missing_tab", "unavailable_live_sessions", "schedule_lookups", "live_query"],
);
const channelProfileKeys = keySchema([], ["handle", "description", "country", "joined_date"]);
const liveQueryKeys = keySchema(["channel_id", "source", "statuses", "exhausted", "access_restricted", "page_count"]);
const unavailableLiveSessionKeys = keySchema(["video_id", "channel_id", "reason"]);

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityResult} */
export function validateCommunityResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, communityResponseKeys);
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
  assertResponseKeys(record, contentResponseKeys);
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
  assertResponseKeys(record, channelResponseKeys);
  const profile = recordField(record, "profile");
  assertResponseKeys(profile, channelProfileKeys);
  const liveSessions = arrayField(record, "live_sessions").map(validateLiveSession);
  const unavailable = Object.hasOwn(record, "unavailable_live_sessions")
    ? arrayField(record, "unavailable_live_sessions").map(validateUnavailableLiveSession)
    : undefined;
  if (unavailable != null) {
    if (unavailable.length > maxScheduleMetadataLookups || (record.missing_tab === true && unavailable.length > 0)) {
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
    ...validateScheduleLookups(record),
    ...(record.live_query === undefined ? {} : { live_query: validateLiveQuery(record, unavailable?.length ?? 0) }),
  };
}

/** 예정 영상 조회 수는 실제 streams 조회(live_query)가 있을 때만 있으며 protocol 상한을 넘지 않습니다.
 * @param {Record<string, unknown>} record
 */
function validateScheduleLookups(record) {
  if (Object.hasOwn(record, "schedule_lookups") !== (record.live_query !== undefined)) {
    throw new RpcResponseError("schedule_lookups must accompany the live query proof");
  }
  if (record.schedule_lookups === undefined) return {};
  const lookups = nonnegativeIntegerField(record, "schedule_lookups");
  if (lookups > maxScheduleMetadataLookups) {
    throw new RpcResponseError("schedule_lookups exceeds the protocol limit");
  }
  return { schedule_lookups: lookups };
}

/**
 * @param {Record<string, unknown>} parent
 * @param {number} unavailableCount
 * @returns {NonNullable<import("./contracts.d.ts").ChannelResult["live_query"]>}
 */
function validateLiveQuery(parent, unavailableCount) {
  const query = recordField(parent, "live_query");
  assertResponseKeys(query, liveQueryKeys);
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
  assertResponseKeys(record, unavailableLiveSessionKeys);
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

const communityPostKeys = keySchema(
  ["postId", "authorId", "authorName", "authorPhoto", "contentText", "publishedText", "likeCount", "commentCount"],
  ["upstreamPostId", "publishedAt", "images", "videoId"],
);
const thumbnailKeys = keySchema(["url", "width", "height"]);
const contentItemKeys = keySchema(["video_id", "channel_id", "title"], ["is_upcoming"]);
const liveSessionKeys = keySchema(["video_id", "channel_id", "status"], ["title", "thumbnail_url", "scheduled_at", "schedule_observed_at", "started_at", "ended_at"]);
const photoKeys = keySchema(["kind", "url", "width", "height"]);

/** @param {unknown} value @returns {import("./contracts.d.ts").CommunityPost} */
function validateCommunityPost(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, communityPostKeys);
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
  assertResponseKeys(record, thumbnailKeys);
  return {
    url: nonemptyStringField(record, "url"),
    width: nonnegativeIntegerField(record, "width"),
    height: nonnegativeIntegerField(record, "height"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ContentItem} */
function validateContentItem(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, contentItemKeys);
  if (Object.hasOwn(record, "is_upcoming") && record.is_upcoming !== true) {
    throw new RpcResponseError("content item is_upcoming must be omitted unless true");
  }
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    title: stringField(record, "title"),
    ...(record.is_upcoming === true ? { is_upcoming: true } : {}),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").LiveSessionItem} */
function validateLiveSession(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, liveSessionKeys);
  const status = validateLiveStatus(record);
  // 재사용 일정의 원래 조회 시각은 시각이 있는 UPCOMING 행에만 붙습니다.
  if (Object.hasOwn(record, "schedule_observed_at") && (status !== "UPCOMING" || !Object.hasOwn(record, "scheduled_at"))) {
    throw new RpcResponseError("schedule_observed_at requires an upcoming scheduled_at");
  }
  return {
    video_id: nonemptyStringField(record, "video_id"),
    channel_id: nonemptyStringField(record, "channel_id"),
    status,
    ...optionalResponseString(record, "title"),
    ...optionalResponseString(record, "thumbnail_url"),
    ...optionalRFC3339(record, "scheduled_at"),
    ...optionalRFC3339(record, "schedule_observed_at"),
    ...optionalRFC3339(record, "started_at"),
    ...optionalRFC3339(record, "ended_at"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelPhotoVariant} */
function validatePhoto(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, photoKeys);
  return {
    kind: validatePhotoKind(record),
    url: nonemptyStringField(record, "url"),
    width: nonnegativeIntegerField(record, "width"),
    height: nonnegativeIntegerField(record, "height"),
  };
}

/** @param {Record<string, unknown>} record */
function pageLimits(record) {
  const maxResults = positiveInteger(record, "max_results");
  if (maxResults > maxResultsLimit) {
    throw new RpcRequestError(`max_results must not exceed ${maxResultsLimit}`);
  }
  const maxPages = positiveInteger(record, "max_pages");
  if (maxPages > maxPagesLimit) {
    throw new RpcRequestError(`max_pages must not exceed ${maxPagesLimit}`);
  }
  return { max_results: maxResults, max_pages: maxPages };
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").Pagination} */
function validatePagination(record) {
  const { page_count, exhausted, continuity, termination_reason } = record;
  return validatedPagination({ page_count, exhausted, continuity, termination_reason });
}

/** @param {Record<string, unknown>} record @returns {import("./contracts.d.ts").LiveStatus} */
function validateLiveStatus(record) {
  const value = nonemptyStringField(record, "status");
  if (value === "LIVE" || value === "UPCOMING" || value === "ENDED") {
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
