import { FetchTransportError } from "./fetch-transport.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";
import { fetchChannelTab } from "./channel-tabs.mjs";
import { textOf, thumbnailsOf } from "./map-posts.mjs";
import { assertVideoRowTitled, hasDurationMarker, hasLiveMarker, hasUpcomingMarker, isVideoLockup, lockupLiveStatus, videoIDOf, videoThumbnailsOf, videoTitleOf } from "./map-lockup.mjs";
import { fetchLiveMetadata } from "./live-metadata.mjs";
import { assertResponseBudget, EncodedArrayBudget, encodedSize, paginationResult, hasContinuation } from "./pagination.mjs";
import { recordScheduleLookup } from "./request-context.mjs";
import { maxScheduleMetadataLookups } from "./rpc-validation-primitives.mjs";

const vanityHandlePathPattern = /^\/(@[^\s/]+)\/?$/u;
const responseReserveBytes = encodedSize({
  protocol_version: 1,
  live_sessions: [],
  profile: {},
  photo: [],
  ...paginationResult({
    pageCount: 1,
    reason: "exhausted",
    continuity: "NOT_APPLICABLE",
  }),
});

/**
 * 지정한 live/metadata 범위만 조회합니다. 시각이 가려진 접근 제한 영상은 별도 목록과 WARN으로 남깁니다.
 * @param {YouTubeJSFetchOptions} [options]
 * @returns {Promise<Omit<import("./contracts.d.ts").ChannelResult, "protocol_version">>}
 */
export async function fetchChannelFeed({
  channelId: id,
  kind,
  maxScheduleLookups = maxScheduleMetadataLookups,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
  scheduleCache,
  scheduledFor,
} = {}) {
  if (kind !== "live" && kind !== "metadata") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "channel kind must be live or metadata");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "innertube client is required");
  }
  if (!Number.isSafeInteger(maxScheduleLookups) || maxScheduleLookups < 0 || maxScheduleLookups > maxScheduleMetadataLookups) {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "schedule lookup budget is outside the protocol limit");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const slotAt = Date.parse(scheduledFor ?? "");
  const cache = kind === "live" && Number.isFinite(slotAt) ? scheduleCache : undefined;
  // upstream을 기다리기 전에 슬롯을 기록해, 이 요청보다 늦게 끝난 앞 슬롯의 조회가 캐시를 채우지 못하게 합니다.
  cache?.beginSlot(id, slotAt);
  const channel = await runUpstream(() => innertube.getChannel(id));
  // Metadata 작업은 streams/player 장애와 독립적이며 live 작업에는 about 조회가 필요 없습니다.
  const about = kind === "metadata" ? await runUpstream(() => channel.getAbout()) : {};
  const { liveFeed, missingTab } = kind === "live"
    ? await fetchLiveFeed(channel)
    : { liveFeed: { videos: [] }, missingTab: false };
  const exhausted = kind !== "live" || missingTab ||
    (!hasContinuation(liveFeed) && readUpstream(() => "has_continuation" in liveFeed && liveFeed.has_continuation === false));
  const pagination = paginationResult({ pageCount: 1, reason: exhausted ? "exhausted" : "max_pages", continuity: "NOT_APPLICABLE" });
  const query = kind === "live" && !missingTab ? {
    channel_id: id, source: "streams", statuses: ["ENDED", "LIVE", "UPCOMING"], exhausted, page_count: 1,
  } : undefined;
  // access_restricted=true가 한 byte 짧으므로 hydration 전에는 안전한 하한만 센다.
  const reserveBytes = encodedSize({ protocol_version: 1, live_sessions: [], profile: {}, photo: [], ...pagination,
    ...(query === undefined ? {} : { schedule_lookups: 0, live_query: { ...query, access_restricted: true } }) });
  const { sessions: liveSessions, minimumBytes } = collectLiveSnapshot(liveFeed, id, maxSuccessResponseBytes, reserveBytes);
  const { unavailable, lookups } = await enrichUpcomingSchedules(
    liveSessions, innertube, minimumBytes, maxSuccessResponseBytes, maxScheduleLookups, cache, slotAt);
  const unavailableIDs = new Set(unavailable.map((item) => item.video_id));
  if (unavailable.length > 0) {
    logUnavailableSchedules(id, unavailable);
  }
  // streams 조회 한 페이지의 현행 호출 예산을 유지한다. continuation이 남거나
  // 종료 플래그가 없으면 전체 범위를 완료한 것으로 주장하지 않는다.
  return {
    live_sessions: liveSessions.filter((session) => !unavailableIDs.has(session.video_id)),
    ...(unavailable.length === 0 ? {} : { unavailable_live_sessions: unavailable }),
    profile: kind === "metadata" ? mapProfile(channel, about) : {},
    photo: kind === "metadata" ? mapPhoto(channel, about) : [],
    ...pagination,
    ...(missingTab ? { missing_tab: true } : {}),
    ...(kind === "live" && !missingTab ? { schedule_lookups: lookups } : {}),
    ...(kind === "live" && !missingTab ? { live_query: {
      channel_id: id, source: "streams", statuses: ["ENDED", "LIVE", "UPCOMING"],
      exhausted, access_restricted: unavailable.length > 0, page_count: 1,
    } } : {}),
  };
}

async function fetchLiveFeed(channel) {
  if (typeof channel.getLiveStreams !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "streams tab loader is unavailable");
  }
  const tab = await fetchChannelTab(channel, "streams", () => channel.getLiveStreams());
  return tab.missing === true
    ? { liveFeed: { videos: [] }, missingTab: true }
    : { liveFeed: tab.feed, missingTab: false };
}

function logUnavailableSchedules(channelId, unavailable) {
  process.stderr.write(`${JSON.stringify({
    time: new Date().toISOString(),
    level: "WARN",
    source: "youtubejs/fetch-channel",
    msg: "YouTube live schedules unavailable",
    event: "youtubejs_live_schedule_unavailable",
    channel_id: channelId,
    reason: "access_restricted",
    video_ids: unavailable.map((item) => item.video_id),
    count: unavailable.length,
  })}\n`);
}

/**
 * 재사용은 collector lease 슬롯 시각이 있는 요청에서만 합니다. 캐시는 그 슬롯 시각을 조회 시각으로 기록해,
 * API가 재사용 행을 원래 관측과 같은 시계로 더 새 관측과 비교하게 합니다.
 * @param {import("./schedule-cache.mjs").ScheduleLookupCache | undefined} cache 슬롯 시각이 있는 live 요청에서만 전달합니다.
 * @param {number} slotAt
 */
async function enrichUpcomingSchedules(sessions, innertube, minimumBytes, maxSuccessResponseBytes, maxLookups, cache, slotAt) {
  const candidates = new Map();
  const confirmedIDs = new Set();
  let liveRowCount = 0;
  for (const session of sessions) {
    if (session.status !== "UPCOMING" || session.scheduled_at != null) {
      confirmedIDs.add(session.video_id);
      // 목록이 시각이나 다른 상태를 직접 보였으므로, 다시 미확정으로 보이면 player로 새로 확인합니다.
      cache?.forget(session.channel_id, session.video_id, slotAt);
      liveRowCount++;
      continue;
    }
    const rows = candidates.get(session.video_id) ?? [];
    rows.push(session);
    candidates.set(session.video_id, rows);
  }
  // 조회 상한은 collector의 호출 예산이므로 초과는 응답 형상 오류가 아니라 자원 한도입니다.
  if (candidates.size > maxLookups) {
    const error = new Error("upcoming schedule lookups exceed the requested upstream call budget");
    error.code = "response_too_large";
    throw error;
  }

  const unavailable = new Map();
  let pendingCount = candidates.size;
  let lookups = 0;
  for (const [videoId, rows] of candidates) {
    const channelId = rows[0].channel_id;
    // 상한 검사는 후보 전체에 그대로 적용하고, 재사용은 실제 player 요청만 줄입니다.
    // 더 앞선 슬롯에서 확인한 값만 재사용하므로, 같은 슬롯의 재시도와 겹친 요청은 player로 다시 확인합니다.
    const reused = cache?.reuse(channelId, videoId, slotAt);
    /** @type {{ scheduleUnavailableReason?: string, isLive?: boolean, isUpcoming?: boolean, startTimestamp?: string | null }} */
    let metadata;
    if (reused === undefined) {
      lookups++;
      recordScheduleLookup();
      cache?.forget(channelId, videoId, slotAt);
      metadata = await fetchLiveMetadata(innertube, videoId);
    } else {
      metadata = reused;
    }
    const identity = { video_id: videoId, channel_id: channelId };
    minimumBytes -= encodedSize(identity);
    pendingCount--;
    if (metadata.scheduleUnavailableReason === "access_restricted") {
      // 같은 ID의 미해결 행은 함께 분류하므로 기존 정상 관측만 충돌할 수 있습니다.
      // 접근 제한 행을 제외하기 전에 이 충돌을 확인해야 합니다.
      if (confirmedIDs.has(videoId)) {
        const error = new Error("unavailable live session conflicts with another row");
        error.code = "parser_drift";
        throw error;
      }
      const item = {
        ...identity,
        reason: metadata.scheduleUnavailableReason,
      };
      unavailable.set(videoId, item);
      minimumBytes += encodedSize(item);
    } else {
      for (const session of rows) {
        if (metadata.isLive === true && metadata.isUpcoming !== true) {
          session.status = "LIVE";
          if (metadata.startTimestamp != null) {
            session.started_at = metadata.startTimestamp;
          }
        } else {
          if (metadata.isUpcoming === false || metadata.startTimestamp == null) {
            const error = new Error("upcoming live session has no machine-readable schedule");
            error.code = "parser_drift";
            throw error;
          }
          session.scheduled_at = metadata.startTimestamp;
          // 재사용한 시각은 원래 확인 슬롯의 관측입니다. API는 이 시각으로 더 새 일정과 비교합니다.
          if (reused !== undefined) {
            session.schedule_observed_at = reused.observedAt;
          }
        }
        minimumBytes += encodedSize(session);
      }
      minimumBytes += rows.length - 1;
      liveRowCount += rows.length;
    }
    // 검증을 통과한 새 결과만 기록합니다. 접근 제한·LIVE 판정과 실패한 조회는 항목이 비어 다음 poll이 다시 확인합니다.
    if (reused === undefined) {
      cache?.remember(channelId, videoId, metadata, slotAt);
    }
    // 원래 단일 배열 하한에서 identity만 실제 표현으로 교체합니다. 미해결 중복은
    // 여전히 하나로 셉니다. 두 배열이 모두 차면 분리 지점의 쉼표 하나를 뺍니다.
    const splitBytes = unavailable.size === 0 ? 0
      : encodedSize({ unavailable_live_sessions: [] }) - 1 - (liveRowCount + pendingCount > 0 ? 1 : 0);
    if (minimumBytes + splitBytes > maxSuccessResponseBytes) {
      const error = new Error("live snapshot minimum exceeds success response limit");
      error.code = "response_too_large";
      throw error;
    }
  }

  if (sessions.some((session) => session.status === "UPCOMING" && session.scheduled_at == null && !unavailable.has(session.video_id))) {
    const error = new Error("upcoming live session remains incomplete");
    error.code = "parser_drift";
    throw error;
  }
  return { unavailable: [...unavailable.values()], lookups };
}

export function* liveSessionRows(feed, channelId) {
  const rows = readUpstream(() => feed?.videos);
  if (!Array.isArray(rows)) {
    const error = new Error("live page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
  for (const row of rows) {
    assertVideoRowTitled(row);
    const videoId = videoIDOf(row);
    const status = mapLiveStatus(row);
    if (videoId === "") {
      const err = new Error("live row is missing video id");
      err.code = "parser_drift";
      throw err;
    }
    if (status === "") {
      const err = new Error("live row has unknown status");
      err.code = "parser_drift";
      throw err;
    }
    const title = videoTitleOf(row);
    const thumbnail = firstThumbnail(videoThumbnailsOf(row));
    const thumbnailURL = optionalHTTPSURL(thumbnail?.url);
    const rowChannelID = textOf(row?.author?.id || channelId).trim() || channelId;
    if (rowChannelID !== channelId) {
      const error = new Error("live row channel identity does not match the request");
      error.code = "parser_drift";
      throw error;
    }
    yield {
      video_id: videoId,
      channel_id: rowChannelID,
      status,
      ...(title === "" ? {} : { title }),
      ...(thumbnailURL === "" ? {} : { thumbnail_url: thumbnailURL }),
      scheduled_at: scheduledAtOf(row),
    };
  }
}


export function mapProfile(channel, about) {
  // 최신 about 응답은 AboutChannel.metadata에 필드를 보관합니다.
  const profile = about?.type === "AboutChannel" ? about.metadata : about;
  const providedHandle = channel?.header?.channel_handle;
  let metadataHandle = null;
  if (providedHandle == null && typeof channel?.metadata?.vanity_channel_url === "string") {
    try {
      const url = new URL(channel.metadata.vanity_channel_url);
      if ((url.protocol === "https:" || url.protocol === "http:") &&
          (url.hostname === "www.youtube.com" || url.hostname === "youtube.com") &&
          url.port === "" && url.username === "" && url.password === "") {
        // legacy /user·/c 주소는 handle을 증명하지 않습니다.
        const match = vanityHandlePathPattern.exec(decodeURIComponent(url.pathname));
        metadataHandle = match?.[1] ?? null;
      }
    } catch {
      // 파싱할 수 없는 vanity URL은 handle 값이 없는 상태로 보존합니다.
    }
  }
  return {
    handle: optionalText(providedHandle ?? metadataHandle),
    description: optionalText(profile?.description ?? channel?.metadata?.description),
    country: optionalText(profile?.country),
    joined_date: optionalText(profile?.joined_date),
  };
}

export function mapPhoto(channel, about) {
  const variants = [];
  const header = channel?.header;
  const headerImage = header?.type === "PageHeader" ? header.content?.image : undefined;
  const avatar = firstThumbnail(headerImage?.type === "DecoratedAvatarView" ? headerImage.avatar?.image : headerImage?.image)
    ?? firstThumbnail(header?.author?.thumbnails)
    ?? firstThumbnail(channel?.metadata?.avatar)
    ?? firstThumbnail(about?.avatar);
  if (avatar != null) {
    variants.push({ kind: "avatar", url: avatar.url, width: avatar.width, height: avatar.height });
  }
  const banner = firstThumbnail(header?.type === "PageHeader" ? header.content?.banner?.image : header?.banner);
  if (banner != null) {
    variants.push({ kind: "banner", url: banner.url, width: banner.width, height: banner.height });
  }
  return variants;
}

// 표시가 없는 행은 종료로 추정하지 않고 ""로 두어 drift로 거부합니다.
function mapLiveStatus(row) {
  if (hasLiveMarker(row)) {
    return "LIVE";
  }
  if (hasUpcomingMarker(row)) {
    return "UPCOMING";
  }
  if (isVideoLockup(row)) {
    return lockupLiveStatus(row);
  }
  return hasDurationMarker(row) ? "ENDED" : "";
}


function optionalText(value) {
  const text = textOf(value).trim();
  return text === "" ? null : text;
}

function firstThumbnail(value) {
  const rows = thumbnailsOf(value);
  return rows[0] ?? null;
}

// 목록이 주는 기계가독 시각은 Video·GridVideo의 upcomingEventData(SDK upcoming)뿐입니다. 시작·종료 시각은 목록에 없습니다.
function scheduledAtOf(row) {
  const upcoming = row?.upcoming;
  return upcoming instanceof Date && Number.isFinite(upcoming.getTime()) ? upcoming.toISOString() : undefined;
}

function optionalHTTPSURL(value) {
  try {
    const parsed = new URL(String(value ?? "").trim());
    if (parsed.protocol !== "https:" || parsed.username !== "" || parsed.password !== "") {
      return "";
    }
    return parsed.toString();
  } catch {
    return "";
  }
}

function collectLiveSnapshot(feed, channelId, maxSuccessResponseBytes, reserveBytes) {
  const budget = new EncodedArrayBudget(maxSuccessResponseBytes, reserveBytes);
  const unresolvedIDs = new Set();
  const sessions = [];
  for (const session of liveSessionRows(feed, channelId)) {
    /** @type {Partial<typeof session> | null} */
    let minimum = session;
    if (session.status === "UPCOMING" && session.scheduled_at == null) {
      // 제한 영상은 중복 행이 하나의 unavailable identity로 축소될 수 있습니다.
      // LIVE 전환도 가능하므로 status/reason/제목 없이 고유 identity만 하한에 넣습니다.
      if (unresolvedIDs.has(session.video_id)) {
        minimum = null;
      } else {
        unresolvedIDs.add(session.video_id);
        minimum = { video_id: session.video_id, channel_id: session.channel_id };
      }
    }
    // 실제 두 배열로 나뉘면 추가되는 필드·괄호가 여기서 센 쉼표보다 큽니다.
    if (minimum != null && budget.tryAppend(minimum) === "WOULD_EXCEED") {
      const error = new Error("live snapshot minimum exceeds success response limit");
      error.code = "response_too_large";
      throw error;
    }
    sessions.push(session);
  }
  return { sessions, minimumBytes: reserveBytes + budget.encodedItemsBytes() };
}
