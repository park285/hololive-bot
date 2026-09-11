const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

export async function fetchLiveMetadata(innertube, videoId) {
  const id = String(videoId ?? "").trim();
  if (id === "") {
    throw parserDrift("player metadata video id is required");
  }
  if (innertube == null || typeof innertube.actions?.execute !== "function") {
    throw parserDrift("raw player metadata lookup is unavailable");
  }

  const response = await innertube.actions.execute("/player", {
    videoId: id,
    racyCheckOk: true,
    contentCheckOk: true,
    parse: false,
  });
  if (!isRecord(response)) {
    throw parserDrift("raw player response is not an object");
  }
  if (typeof response.success !== "boolean") {
    throw parserDrift("raw player response success flag is missing");
  }
  if (!Number.isSafeInteger(response.status_code) || response.status_code < 100 || response.status_code > 599) {
    throw parserDrift("raw player response status is invalid");
  }
  if (!response.success) {
    throw new PlayerHTTPError(response.status_code);
  }
  if (response.status_code !== 200) {
    throw parserDrift("successful raw player response has an invalid status");
  }

  return parseRawLiveMetadata(response.data, id);
}

/** 요청 영상의 엄격한 live 상태·예정 시각과, 시각이 가려진 경우의 확인된 접근 제한 사유를 추출합니다. */
export function parseRawLiveMetadata(raw, expectedVideoId) {
  if (!isRecord(raw)) {
    throw parserDrift("raw player data is not an object");
  }
  const details = raw.videoDetails;
  if (!isRecord(details)) {
    throw parserDrift("raw player videoDetails is missing");
  }
  if (details.videoId !== expectedVideoId) {
    throw parserDrift("raw player video identity does not match the request");
  }

  const isLive = optionalBoolean(details, "isLive");
  const isUpcoming = optionalBoolean(details, "isUpcoming");
  const isLiveContent = optionalBoolean(details, "isLiveContent");
  if (isLive === true && isUpcoming === true) {
    throw parserDrift("raw player live state is contradictory");
  }

  let startTimestamp;
  if (raw.microformat != null) {
    if (!isRecord(raw.microformat)) {
      throw parserDrift("raw player microformat is not an object");
    }
    const renderer = raw.microformat.playerMicroformatRenderer;
    if (renderer != null) {
      if (!isRecord(renderer)) {
        throw parserDrift("raw player microformat renderer is not an object");
      }
      const liveDetails = renderer.liveBroadcastDetails;
      if (liveDetails != null) {
        if (!isRecord(liveDetails)) {
          throw parserDrift("raw player liveBroadcastDetails is not an object");
        }
        if (Object.hasOwn(liveDetails, "startTimestamp")) {
          startTimestamp = normalizedRFC3339(liveDetails.startTimestamp);
        }
      }
    }
  }

  const offlineSlateTimestamp = offlineSlateStartTimestamp(raw.playabilityStatus, expectedVideoId);
  if (startTimestamp != null && offlineSlateTimestamp != null && startTimestamp !== offlineSlateTimestamp) {
    throw parserDrift("raw player schedule timestamps are contradictory");
  }
  startTimestamp ??= offlineSlateTimestamp;

  return {
    videoId: expectedVideoId,
    ...(isLive == null ? {} : { isLive }),
    ...(isUpcoming == null ? {} : { isUpcoming }),
    ...(isLiveContent == null ? {} : { isLiveContent }),
    ...(startTimestamp == null ? {} : { startTimestamp }),
    ...(startTimestamp == null && isUpcoming === true && isLiveContent === true && hasPaidAccessRestriction(raw.playabilityStatus)
      ? { scheduleUnavailableReason: "access_restricted" }
      : {}),
  };
}

// 멤버십 안내가 예정 시각을 가린 실제 응답만 분류하며 번역된 reason 문자열은 판정에 쓰지 않습니다.
function hasPaidAccessRestriction(playabilityStatus) {
  if (playabilityStatus?.status !== "UNPLAYABLE" || playabilityStatus.errorScreen == null) {
    return false;
  }
  const screen = playabilityStatus.errorScreen;
  if (!isRecord(screen)) {
    throw parserDrift("raw player errorScreen is not an object");
  }
  if (!Object.hasOwn(screen, "playerLegacyDesktopYpcOfferRenderer")) {
    return false;
  }
  if (!isRecord(screen.playerLegacyDesktopYpcOfferRenderer)) {
    throw parserDrift("raw player paid offer renderer is not an object");
  }
  return true;
}

// 실제 UPCOMING 응답은 microformat 대신 offline slate에 epoch seconds를 제공할 수 있습니다.
function offlineSlateStartTimestamp(playabilityStatus, expectedVideoId) {
  if (playabilityStatus == null) {
    return undefined;
  }
  if (!isRecord(playabilityStatus)) {
    throw parserDrift("raw player playabilityStatus is not an object");
  }
  const streamability = playabilityStatus.liveStreamability;
  if (streamability == null) {
    return undefined;
  }
  if (!isRecord(streamability)) {
    throw parserDrift("raw player liveStreamability is not an object");
  }
  const renderer = streamability.liveStreamabilityRenderer;
  if (renderer == null) {
    return undefined;
  }
  if (!isRecord(renderer)) {
    throw parserDrift("raw player liveStreamabilityRenderer is not an object");
  }
  if (Object.hasOwn(renderer, "videoId") && renderer.videoId !== expectedVideoId) {
    throw parserDrift("raw player offline slate video identity does not match the request");
  }
  const offlineSlate = renderer.offlineSlate;
  if (offlineSlate == null) {
    return undefined;
  }
  if (!isRecord(offlineSlate)) {
    throw parserDrift("raw player offlineSlate is not an object");
  }
  const slateRenderer = offlineSlate.liveStreamOfflineSlateRenderer;
  if (slateRenderer == null) {
    return undefined;
  }
  if (!isRecord(slateRenderer)) {
    throw parserDrift("raw player liveStreamOfflineSlateRenderer is not an object");
  }
  if (!Object.hasOwn(slateRenderer, "scheduledStartTime")) {
    return undefined;
  }
  return normalizedEpochSeconds(slateRenderer.scheduledStartTime);
}

function optionalBoolean(record, key) {
  if (!Object.hasOwn(record, key)) {
    return undefined;
  }
  if (typeof record[key] !== "boolean") {
    throw parserDrift(`raw player ${key} is not boolean`);
  }
  return record[key];
}

function normalizedRFC3339(value) {
  if (typeof value !== "string") {
    throw parserDrift("raw player startTimestamp is not a string");
  }
  const match = rfc3339Pattern.exec(value);
  if (match == null || !validDateTime(match)) {
    throw parserDrift("raw player startTimestamp is not RFC3339");
  }
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    throw parserDrift("raw player startTimestamp is invalid");
  }
  return new Date(parsed).toISOString();
}

function normalizedEpochSeconds(value) {
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) {
    throw parserDrift("raw player scheduledStartTime is not epoch seconds");
  }
  const seconds = Number(value);
  if (!Number.isSafeInteger(seconds)) {
    throw parserDrift("raw player scheduledStartTime is outside the safe integer range");
  }
  const milliseconds = seconds * 1000;
  if (!Number.isSafeInteger(milliseconds)) {
    throw parserDrift("raw player scheduledStartTime is outside the supported date range");
  }
  const parsed = new Date(milliseconds);
  if (!Number.isFinite(parsed.getTime())) {
    throw parserDrift("raw player scheduledStartTime is invalid");
  }
  const normalized = parsed.toISOString();
  if (!rfc3339Pattern.test(normalized)) {
    throw parserDrift("raw player scheduledStartTime is outside the supported RFC3339 range");
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
  const monthDays = [31, isLeapYear(year) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day <= monthDays[month - 1];
}

function isLeapYear(year) {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function parserDrift(message) {
  const error = new Error(message);
  error.code = "parser_drift";
  return error;
}

function isRecord(value) {
  return value != null && typeof value === "object" && !Array.isArray(value);
}

class PlayerHTTPError extends Error {
  constructor(status) {
    super(`raw player request failed with status code ${status}`);
    this.status = status;
  }
}
