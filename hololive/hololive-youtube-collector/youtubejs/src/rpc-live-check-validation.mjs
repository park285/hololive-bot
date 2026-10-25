// @ts-check

import { RpcResponseError } from "./rpc-errors.mjs";
import {
  assertRequestKeys,
  assertResponseKeys,
  keySchema,
  maxChannelIdentifierBytes,
  maxVideoIdentifierBytes,
  optionalResponseBoolean,
  optionalResponseTimestamp,
  positiveInteger,
  protocolVersion,
  requestIdentifier,
  requestRecord,
  requiredResponseBoolean,
  responseIdentifier,
  responseProtocolVersion,
  responseRecord,
} from "./rpc-validation-primitives.mjs";

const channelLiveCheckRequestKeys = keySchema(["protocol_version", "channel_id", "max_success_response_bytes"]);
const videoLiveCheckRequestKeys = keySchema(["protocol_version", "video_id", "max_success_response_bytes"]);

/** @param {unknown} value @returns {import("./contracts.d.ts").ChannelLiveCheckRequest} */
function validateChannelLiveCheckRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, channelLiveCheckRequestKeys);
  return {
    protocol_version: protocolVersion(record),
    channel_id: requestIdentifier(record, "channel_id", maxChannelIdentifierBytes),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

/** @param {unknown} value @returns {import("./contracts.d.ts").VideoLiveCheckRequest} */
function validateVideoLiveCheckRequest(value) {
  const record = requestRecord(value);
  assertRequestKeys(record, videoLiveCheckRequestKeys);
  return {
    protocol_version: protocolVersion(record),
    video_id: requestIdentifier(record, "video_id", maxVideoIdentifierBytes),
    max_success_response_bytes: positiveInteger(record, "max_success_response_bytes"),
  };
}

const channelLiveCheckResponseKeys = keySchema(
  ["protocol_version", "channel_id", "outcome", "channel_identity_confirmed"],
  ["selected_video_id", "unknown_reason"],
);

/**
 * 채널 확인 결과는 pagination 없이 자체 불변식으로 검증합니다. 확인된 결과만 identity를 확정하고,
 * CHANNEL_PAGE는 선택 영상이 없으며 LIVE_VIDEO·UPCOMING_VIDEO는 선택 영상이 있어야 합니다.
 * @param {unknown} value
 * @returns {import("./contracts.d.ts").ChannelLiveCheckResult}
 */
export function validateChannelLiveCheckResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, channelLiveCheckResponseKeys);
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

const videoLiveCheckResponseKeys = keySchema(
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
    "published_at",
    "unknown_reason",
  ],
);

/**
 * 영상 확인 결과를 검증합니다. method는 availability에서만 결정되고, UNKNOWN일 때만 사유가 있습니다.
 * identity가 확인되지 않은 결과는 다른 영상의 사실을 싣지 않으며, 신뢰 가능한 수명 사실은 서로 모순되지 않아야 합니다.
 * @param {unknown} value
 * @returns {import("./contracts.d.ts").VideoLiveCheckResult}
 */
export function validateVideoLiveCheckResponse(value) {
  const record = responseRecord(value);
  assertResponseKeys(record, videoLiveCheckResponseKeys);
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
    published_at: optionalResponseTimestamp(record, "published_at"),
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
  // 예정 시각은 대기 상태가 확인된 경우에만 근거이므로 확인 사실 없이 내보내지 않습니다.
  if (facts.scheduled_at !== undefined && facts.waiting_state_confirmed !== true) {
    throw new RpcResponseError("video live check scheduled_at requires a confirmed waiting state");
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
    ...(facts.published_at === undefined ? {} : { published_at: facts.published_at }),
    availability,
    method,
    ...(unknownReason === undefined ? {} : { unknown_reason: unknownReason }),
  };
}

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
