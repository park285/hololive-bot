// !라이브 채널·영상 확인 관측을 raw innertube 응답의 사실 필드로만 판정합니다.
// 판정 순서는 identity → boolean·시각 사실 → 모순 → 가용성이며, 해석할 수 없는 응답은 예외 대신
// UNKNOWN 결과로 돌려줍니다. upstream 요청 실패만 typed RPC 실패로 전파합니다.
import { FetchTransportError } from "./fetch-transport.mjs";
import { currentRequestSignal } from "./request-context.mjs";

/** @typedef {Omit<import("./contracts.d.ts").VideoLiveCheckResult, "protocol_version">} VideoCheck */
/** @typedef {Pick<VideoCheck, "availability" | "method" | "unknown_reason">} AvailabilityFacts */

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const maxVideoIdBytes = 128;
const maxChannelIdBytes = 256;
const playabilityStatuses = new Set(["OK", "UNPLAYABLE", "LIVE_STREAM_OFFLINE", "LOGIN_REQUIRED", "ERROR"]);

/** raw 구조 해석 실패입니다. 호출 경계에서 UNKNOWN 사유로만 변환합니다. */
class RawStructureError extends Error {
  constructor(reason, message) {
    super(message);
    this.name = "RawStructureError";
    this.reason = reason;
  }
}

/**
 * 채널 확인 전용 Innertube를 만듭니다. 세션을 로컬에서 만들고 config 조회를 끄므로 생성 자체는 upstream을 호출하지 않습니다.
 * fetchImpl은 재시도 없는 단일 시도 transport여야 합니다.
 * @param {{ fetchImpl?: import("./upstream-feeds.d.ts").InnertubeFetch }} [options]
 */
export async function createLiveCheckInnertube({ fetchImpl } = {}) {
  if (typeof fetchImpl !== "function") {
    throw new FetchTransportError(
      "helper_internal_invariant",
      "INTERNAL",
      "live check single-attempt transport is unavailable",
    );
  }
  const { Innertube } = await import("youtubei.js");
  return Innertube.create({
    retrieve_player: false,
    generate_session_locally: true,
    enable_session_cache: false,
    retrieve_innertube_config: false,
    fetch: fetchImpl,
  });
}

/**
 * `/channel/{id}/live`를 resolve_url 1회로 해석하고, 영상으로 연결되면 해당 영상 player를 최대 1회 조회합니다.
 * HTML·browse 보완이나 재시도는 하지 않습니다.
 */
export async function fetchChannelLiveCheck(innertube, channelId, clock = Date.now, proof = undefined) {
  assertInnertube(innertube);
  const resolved = await executeRaw(innertube, "/navigation/resolve_url", {
    url: `https://www.youtube.com/channel/${encodeURIComponent(channelId)}/live`,
    parse: false,
  });
  if (resolved.ok === false) {
    return unknownChannel(channelId, undefined, resolved.reason, false);
  }
  const target = classifyResolvedChannel(resolved.data, channelId);
  if (target.kind !== "watch") {
    return target.result;
  }
  const player = await executePlayer(innertube, target.videoId, proof);
  if (player.ok === false) {
    return unknownChannel(channelId, target.videoId, player.reason, false);
  }
  return classifyChannelPlayer(player.data, channelId, target.videoId, clock());
}

/** 요청 영상 player를 1회 조회해 영상 확인 사실을 만듭니다. */
export async function fetchVideoLiveCheck(innertube, videoId, clock = Date.now, proof = undefined) {
  assertInnertube(innertube);
  const player = await executePlayer(innertube, videoId, proof);
  if (player.ok === false) {
    return unknownVideo(videoId, player.reason);
  }
  return parseVideoLiveCheck(player.data, videoId, clock());
}

/**
 * resolve_url raw 응답을 분류합니다. 정상 endpoint의 WEB_PAGE_TYPE_CHANNEL과 요청 채널과 같은 browseId만
 * CHANNEL_PAGE이고, WATCH endpoint는 player 확인이 필요한 영상 ID를 돌려줍니다.
 * @returns {{ kind: "result", result: object } | { kind: "watch", videoId: string }}
 */
export function classifyResolvedChannel(raw, channelId) {
  try {
    return classifyResolvedEndpoint(raw, channelId);
  } catch (error) {
    if (error instanceof RawStructureError) {
      return { kind: "result", result: unknownChannel(channelId, undefined, error.reason, false) };
    }
    throw error;
  }
}

/**
 * /live가 고른 영상의 player 응답으로 채널 확인 결과를 만듭니다. 요청 채널의 현재 LIVE 사실이면 LIVE_VIDEO,
 * 요청 채널 예정 영상의 대기 상태가 확인되면 UPCOMING_VIDEO, 그 밖에는 UNKNOWN입니다.
 */
export function classifyChannelPlayer(raw, channelId, selectedVideoId, nowMs) {
  const analysis = analyzePlayer(raw, selectedVideoId, nowMs);
  const video = analysis.result;
  if (!video.identity_confirmed) {
    return unknownChannel(channelId, selectedVideoId, video.unknown_reason, false);
  }
  if (video.channel_id !== channelId) {
    return unknownChannel(channelId, selectedVideoId, "identity_mismatch", false);
  }
  if (video.unknown_reason !== undefined && video.unknown_reason !== "availability_unclassified") {
    return unknownChannel(channelId, selectedVideoId, video.unknown_reason, true);
  }
  if (video.is_live === true || video.is_live_now === true) {
    return {
      channel_id: channelId,
      outcome: "LIVE_VIDEO",
      selected_video_id: selectedVideoId,
      channel_identity_confirmed: true,
    };
  }
  if (analysis.waiting) {
    return {
      channel_id: channelId,
      outcome: "UPCOMING_VIDEO",
      selected_video_id: selectedVideoId,
      channel_identity_confirmed: true,
    };
  }
  return unknownChannel(channelId, selectedVideoId, "not_waiting_state", true);
}

/** raw player 응답에서 영상 확인 사실과 가용성을 추출합니다. nowMs는 응답 수신 뒤의 helper 시각입니다. */
export function parseVideoLiveCheck(raw, videoId, nowMs) {
  return analyzePlayer(raw, videoId, nowMs).result;
}

/** @returns {{ result: VideoCheck, waiting: boolean }} */
function analyzePlayer(raw, videoId, nowMs) {
  if (!Number.isFinite(nowMs)) {
    throw new Error("live check clock is invalid");
  }
  const identity = playerIdentity(raw, videoId);
  if (identity.ok === false) {
    return { result: unknownVideo(videoId, identity.reason), waiting: false };
  }
  let facts;
  let playability;
  try {
    playability = readPlayability(raw.playabilityStatus, videoId);
    facts = readFacts(identity.details, raw.microformat);
  } catch (error) {
    if (!(error instanceof RawStructureError)) {
      throw error;
    }
    if (error.reason === "identity_mismatch") {
      return { result: unknownVideo(videoId, "identity_mismatch"), waiting: false };
    }
    return {
      result: {
        video_id: videoId,
        channel_id: identity.channelId,
        identity_confirmed: true,
        availability: "UNKNOWN",
        method: "unknown",
        unknown_reason: error.reason,
      },
      waiting: false,
    };
  }

  /** @type {Omit<VideoCheck, "availability" | "method">} */
  const base = {
    video_id: videoId,
    channel_id: identity.channelId,
    identity_confirmed: true,
    ...optionalField("is_live", facts.isLive),
    ...optionalField("is_live_now", facts.isLiveNow),
    ...optionalField("is_upcoming", facts.isUpcoming),
    ...optionalField("is_live_content", facts.isLiveContent),
    ...optionalField("is_private", facts.isPrivate),
    ...optionalField("has_live_broadcast_details", facts.hasLiveBroadcastDetails),
    ...optionalField("started_at", facts.startedAt),
    ...optionalField("ended_at", facts.endedAt),
  };
  if (hasContradiction(facts, playability, nowMs)) {
    return {
      result: { ...base, availability: "UNKNOWN", method: "unknown", unknown_reason: "contradictory_fields" },
      waiting: false,
    };
  }
  if (facts.isUpcoming === true) {
    delete base.started_at;
  }
  return {
    result: {
      ...base,
      ...optionalField("scheduled_at", isWaitingState(facts, playability) ? (facts.startedAt ?? playability.slateStart) : undefined),
      ...(facts.isUpcoming === true ? { waiting_state_confirmed: isWaitingState(facts, playability) } : {}),
      ...classifyAvailability(facts, playability),
    },
    waiting: isWaitingState(facts, playability),
  };
}

function playerIdentity(raw, videoId) {
  if (!isRecord(raw)) {
    return { ok: false, reason: "structure_unrecognized" };
  }
  const details = raw.videoDetails;
  // 로봇 확인·비공개·오류 응답은 videoDetails 없이 상태만 줄 수 있으므로 identity 부재로 남깁니다.
  if (details === undefined) {
    return { ok: false, reason: "identity_missing" };
  }
  if (!isRecord(details)) {
    return { ok: false, reason: "structure_unrecognized" };
  }
  const rawVideoId = rawIdentifier(details, "videoId", maxVideoIdBytes);
  if (rawVideoId.ok === false) {
    return rawVideoId;
  }
  if (rawVideoId.value !== videoId) {
    return { ok: false, reason: "identity_mismatch" };
  }
  const rawChannelId = rawIdentifier(details, "channelId", maxChannelIdBytes);
  if (rawChannelId.ok === false) {
    return rawChannelId;
  }
  return { ok: true, details, channelId: rawChannelId.value };
}

function rawIdentifier(record, key, maxBytes) {
  const value = record[key];
  if (value === undefined || (typeof value === "string" && value.trim() === "")) {
    return { ok: false, reason: "identity_missing" };
  }
  if (typeof value !== "string" || value.trim() !== value || Buffer.byteLength(value) > maxBytes) {
    return { ok: false, reason: "structure_unrecognized" };
  }
  return { ok: true, value };
}

function readFacts(details, microformat) {
  const broadcast = readBroadcastDetails(microformat);
  return {
    isLive: rawBoolean(details, "isLive"),
    isUpcoming: rawBoolean(details, "isUpcoming"),
    isLiveContent: rawBoolean(details, "isLiveContent"),
    isPrivate: rawBoolean(details, "isPrivate"),
    isLiveNow: broadcast.isLiveNow,
    hasLiveBroadcastDetails: broadcast.present,
    startedAt: broadcast.startedAt,
    endedAt: broadcast.endedAt,
  };
}

// microformat renderer가 없으면 liveBroadcastDetails 존재 여부를 알 수 없으므로 false로 만들지 않습니다.
function readBroadcastDetails(microformat) {
  const absent = { present: undefined, isLiveNow: undefined, startedAt: undefined, endedAt: undefined };
  if (microformat === undefined) {
    return absent;
  }
  const record = rawRecord(microformat, "microformat");
  const renderer = record.playerMicroformatRenderer;
  if (renderer === undefined) {
    return absent;
  }
  const rendererRecord = rawRecord(renderer, "playerMicroformatRenderer");
  const details = rendererRecord.liveBroadcastDetails;
  if (details === undefined) {
    return { ...absent, present: false };
  }
  const detailsRecord = rawRecord(details, "liveBroadcastDetails");
  return {
    present: true,
    isLiveNow: rawBoolean(detailsRecord, "isLiveNow"),
    startedAt: rawTimestamp(detailsRecord, "startTimestamp"),
    endedAt: rawTimestamp(detailsRecord, "endTimestamp"),
  };
}

// 가용성·대기 상태 판정은 구조화된 상태 코드와 renderer만 사용하고 번역된 reason·messages 문자열은 읽지 않습니다.
function readPlayability(playabilityStatus, videoId) {
  const result = { status: undefined, membersOffer: false, offlineSlate: false, slateStart: undefined };
  if (playabilityStatus === undefined) {
    return result;
  }
  const record = rawRecord(playabilityStatus, "playabilityStatus");
  if (record.status !== undefined) {
    if (typeof record.status !== "string" || !playabilityStatuses.has(record.status)) {
      throw new RawStructureError("structure_unrecognized", "raw player status is not recognized");
    }
    result.status = record.status;
  }
  if (record.errorScreen !== undefined) {
    const screen = rawRecord(record.errorScreen, "errorScreen");
    if (Object.hasOwn(screen, "playerLegacyDesktopYpcOfferRenderer")) {
      rawRecord(screen.playerLegacyDesktopYpcOfferRenderer, "playerLegacyDesktopYpcOfferRenderer");
      result.membersOffer = true;
    }
  }
  if (record.liveStreamability === undefined) {
    return result;
  }
  const streamability = rawRecord(record.liveStreamability, "liveStreamability");
  if (streamability.liveStreamabilityRenderer === undefined) {
    return result;
  }
  const renderer = rawRecord(streamability.liveStreamabilityRenderer, "liveStreamabilityRenderer");
  if (renderer.videoId !== undefined) {
    if (typeof renderer.videoId !== "string") {
      throw new RawStructureError("structure_unrecognized", "raw player offline slate video id is not a string");
    }
    if (renderer.videoId !== videoId) {
      throw new RawStructureError("identity_mismatch", "raw player offline slate video identity does not match");
    }
  }
  if (renderer.offlineSlate === undefined) {
    return result;
  }
  const offlineSlate = rawRecord(renderer.offlineSlate, "offlineSlate");
  if (offlineSlate.liveStreamOfflineSlateRenderer === undefined) {
    return result;
  }
  const slate = rawRecord(offlineSlate.liveStreamOfflineSlateRenderer, "liveStreamOfflineSlateRenderer");
  result.offlineSlate = true;
  if (slate.scheduledStartTime !== undefined) {
    result.slateStart = epochSeconds(slate.scheduledStartTime);
  }
  return result;
}

// LIVE와 upcoming/종료의 공존, 종료가 시작보다 이르거나 수신 시각보다 늦음, 두 예정 시각의 불일치는 모두 모순입니다.
// isLive 생략·false와 isLiveNow=false, 유효한 종료 시각의 조합은 종료 근거이므로 모순이 아닙니다.
function hasContradiction(facts, playability, nowMs) {
  if (facts.isPrivate === true && playability.membersOffer) {
    return true;
  }
  if (facts.isLive !== undefined && facts.isLiveNow !== undefined && facts.isLive !== facts.isLiveNow) {
    return true;
  }
  const live = facts.isLive === true || facts.isLiveNow === true;
	if (live && facts.startedAt !== undefined && Date.parse(facts.startedAt) > nowMs) {
		return true;
	}
  if (live && (facts.isUpcoming === true || facts.endedAt !== undefined)) {
    return true;
  }
  if (facts.endedAt !== undefined) {
    if (facts.isUpcoming === true) {
      return true;
    }
    const endedMs = Date.parse(facts.endedAt);
    if (facts.startedAt !== undefined && endedMs < Date.parse(facts.startedAt)) {
      return true;
    }
    if (endedMs > nowMs) {
      return true;
    }
  }
  return facts.startedAt !== undefined && playability.slateStart !== undefined &&
    facts.startedAt !== playability.slateStart;
}

// PUBLIC_UNAVAILABLE은 원시 boolean isPrivate=true일 때만 성립합니다. LOGIN_REQUIRED·ERROR 상태만으로는 공개 불가를 만들지 않습니다.
/** @returns {AvailabilityFacts} */
function classifyAvailability(facts, playability) {
  if (facts.isPrivate === true) {
    return { availability: "PUBLIC_UNAVAILABLE", method: "player_private" };
  }
  if (playability.membersOffer) {
    return { availability: "MEMBERS_ONLY", method: "player_members_only" };
  }
  if (playability.status === "LOGIN_REQUIRED") {
    return unknownAvailability("login_required_unclassified");
  }
  if (playability.status === "ERROR") {
    return unknownAvailability("error_unclassified");
  }
  if (facts.isPrivate === false) {
    return { availability: "PUBLIC", method: "player_public" };
  }
  return unknownAvailability("availability_unclassified");
}

// 대기 상태는 isUpcoming=true, isLiveNow=false, LIVE 아님, 기계 판독 가능한 예정 시각과 offline 상태 또는 slate가 모두 있어야 합니다.
function isWaitingState(facts, playability) {
  return facts.isUpcoming === true &&
    facts.isLiveNow === false &&
    facts.isLive !== true &&
    facts.endedAt === undefined &&
    (facts.startedAt ?? playability.slateStart) !== undefined &&
    (playability.status === "LIVE_STREAM_OFFLINE" || playability.offlineSlate);
}

/** @returns {{ kind: "result", result: object } | { kind: "watch", videoId: string }} */
function classifyResolvedEndpoint(raw, channelId) {
  const response = rawRecord(raw, "resolve_url response");
  const endpoint = rawRecord(response.endpoint, "resolve_url endpoint");
  const commandMetadata = rawRecord(endpoint.commandMetadata, "resolve_url commandMetadata");
  const webMetadata = rawRecord(commandMetadata.webCommandMetadata, "resolve_url webCommandMetadata");
  const pageType = webMetadata.webPageType;
  const hasBrowse = Object.hasOwn(endpoint, "browseEndpoint");
  const hasWatch = Object.hasOwn(endpoint, "watchEndpoint");
  if (pageType === "WEB_PAGE_TYPE_CHANNEL" && !hasWatch) {
    if (!hasBrowse) {
      throw new RawStructureError("identity_missing", "resolve_url channel endpoint has no browse identity");
    }
    const browse = rawRecord(endpoint.browseEndpoint, "resolve_url browseEndpoint");
    const browseId = rawIdentifier(browse, "browseId", maxChannelIdBytes);
    if (browseId.ok === false) {
      throw new RawStructureError(browseId.reason, "resolve_url browse identity is not usable");
    }
    if (browseId.value !== channelId) {
      throw new RawStructureError("identity_mismatch", "resolve_url browse identity does not match");
    }
    return {
      kind: "result",
      result: { channel_id: channelId, outcome: "CHANNEL_PAGE", channel_identity_confirmed: true },
    };
  }
  if (pageType === "WEB_PAGE_TYPE_WATCH" && !hasBrowse) {
    if (!hasWatch) {
      throw new RawStructureError("identity_missing", "resolve_url watch endpoint has no video identity");
    }
    const watch = rawRecord(endpoint.watchEndpoint, "resolve_url watchEndpoint");
    const videoId = rawIdentifier(watch, "videoId", maxVideoIdBytes);
    if (videoId.ok === false) {
      throw new RawStructureError(videoId.reason, "resolve_url watch identity is not usable");
    }
    return { kind: "watch", videoId: videoId.value };
  }
  throw new RawStructureError("structure_unrecognized", "resolve_url endpoint type is not recognized");
}

function playerPayload(videoId) {
  return { videoId, racyCheckOk: true, contentCheckOk: true, parse: false };
}

function executePlayer(innertube, videoId, proof) {
  const send = (token) => executeRaw(innertube, "/player", {
    ...playerPayload(videoId),
    ...(token === undefined ? {} : { serviceIntegrityDimensions: { poToken: token } }),
  });
  return proof === undefined ? send(undefined) : proof.runPlayer(videoId, send, currentRequestSignal());
}

/**
 * raw Actions 요청 1회를 실행합니다. HTTP·네트워크 실패는 typed RPC 실패로, 응답 본문 구조 실패는 UNKNOWN 사유로 돌려줍니다.
 * @returns {Promise<{ ok: true, data: unknown } | { ok: false, reason: "structure_unrecognized" }>}
 */
async function executeRaw(innertube, endpoint, payload) {
  let response;
  try {
    response = await innertube.actions.execute(endpoint, payload);
  } catch (error) {
    if (error instanceof SyntaxError) {
      // 2xx 응답 본문이 JSON이 아니면 요청은 도달했지만 구조를 해석할 수 없는 응답입니다.
      return { ok: false, reason: "structure_unrecognized" };
    }
    throw requestFailure(error);
  }
  if (!isRecord(response) || typeof response.success !== "boolean" || !Number.isSafeInteger(response.status_code)) {
    return { ok: false, reason: "structure_unrecognized" };
  }
  if (!response.success) {
    throw new LiveCheckRequestError(`live check upstream request failed with status code ${response.status_code}`);
  }
  if (response.status_code !== 200) {
    return { ok: false, reason: "structure_unrecognized" };
  }
  return { ok: true, data: response.data };
}

// 취소와 transport가 이미 분류한 실패는 보존하고, youtubei.js HTTP 오류·분류되지 않은 네트워크 오류는 요청 실패로 묶습니다.
function requestFailure(error) {
  if (currentRequestSignal()?.aborted || error instanceof FetchTransportError) {
    return error;
  }
  if (error instanceof Error && error.name === "AbortError") {
    return error;
  }
  return new LiveCheckRequestError("live check upstream request failed", { cause: error });
}

export class LiveCheckRequestError extends Error {
  constructor(message, options) {
    super(message, options);
    this.name = "LiveCheckRequestError";
    this.code = "collection_failed";
    this.failureClass = "TRANSIENT";
  }
}

function assertInnertube(innertube) {
  if (innertube == null || typeof innertube.actions?.execute !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "raw live check lookup is unavailable");
  }
}

function unknownChannel(channelId, selectedVideoId, reason, identityConfirmed) {
  return {
    channel_id: channelId,
    outcome: "UNKNOWN",
    ...(selectedVideoId === undefined ? {} : { selected_video_id: selectedVideoId }),
    channel_identity_confirmed: identityConfirmed,
    unknown_reason: reason,
  };
}

/** @returns {VideoCheck} */
function unknownVideo(videoId, reason) {
  return {
    video_id: videoId,
    identity_confirmed: false,
    availability: "UNKNOWN",
    method: "unknown",
    unknown_reason: reason,
  };
}

/** @returns {AvailabilityFacts} */
function unknownAvailability(reason) {
  return { availability: "UNKNOWN", method: "unknown", unknown_reason: reason };
}

function optionalField(key, value) {
  return value === undefined ? {} : { [key]: value };
}

function rawRecord(value, name) {
  if (!isRecord(value)) {
    throw new RawStructureError("structure_unrecognized", `raw ${name} is not an object`);
  }
  return value;
}

function rawBoolean(record, key) {
  const value = record[key];
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== "boolean") {
    throw new RawStructureError("structure_unrecognized", `raw ${key} is not boolean`);
  }
  return value;
}

function rawTimestamp(record, key) {
  const value = record[key];
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== "string") {
    throw new RawStructureError("structure_unrecognized", `raw ${key} is not a string`);
  }
  const match = rfc3339Pattern.exec(value);
  const parsed = Date.parse(value);
  if (match == null || !validDateTime(match) || !Number.isFinite(parsed)) {
    throw new RawStructureError("structure_unrecognized", `raw ${key} is not RFC3339`);
  }
  return new Date(parsed).toISOString();
}

function epochSeconds(value) {
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) {
    throw new RawStructureError("structure_unrecognized", "raw scheduledStartTime is not epoch seconds");
  }
  const milliseconds = Number(value) * 1000;
  if (!Number.isSafeInteger(milliseconds)) {
    throw new RawStructureError("structure_unrecognized", "raw scheduledStartTime is outside the supported range");
  }
  const parsed = new Date(milliseconds);
  if (!Number.isFinite(parsed.getTime())) {
    throw new RawStructureError("structure_unrecognized", "raw scheduledStartTime is outside the supported range");
  }
  const normalized = parsed.toISOString();
  if (!rfc3339Pattern.test(normalized)) {
    throw new RawStructureError("structure_unrecognized", "raw scheduledStartTime is outside the RFC3339 range");
  }
  return normalized;
}

function validDateTime(match) {
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  if (month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59) {
    return false;
  }
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day <= monthDays[month - 1];
}

function isRecord(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}
