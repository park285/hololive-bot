import { FetchTransportError } from "./fetch-transport.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";
import { fetchChannelTab } from "./channel-tabs.mjs";
import { textOf, thumbnailsOf } from "./map-posts.mjs";
import { isVideoLockup, lockupBadgeTexts, videoIDOf, videoTitleOf } from "./map-lockup.mjs";
import { fetchLiveMetadata } from "./live-metadata.mjs";
import { assertResponseBudget, EncodedArrayBudget, encodedSize, paginationResult, hasContinuation } from "./pagination.mjs";

const maxScheduleMetadataLookups = 32;
const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

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
 */
export async function fetchChannelFeed({
  channelId,
  kind,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
} = {}) {
  const id = String(channelId ?? "").trim();
  if (id === "") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "channel id is required");
  }
  if (kind !== "live" && kind !== "metadata") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "channel kind must be live or metadata");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const channel = await runUpstream(() => innertube.getChannel(id));
  // Metadata 작업은 streams/player 장애와 독립적이며 live 작업에는 about 조회가 필요 없습니다.
  const about = kind === "metadata" && typeof channel.getAbout === "function" ? await runUpstream(() => channel.getAbout()) : {};
  const { liveFeed, missingTab } = kind === "live"
    ? await fetchLiveFeed(channel)
    : { liveFeed: { videos: [] }, missingTab: false };
  const exhausted = kind !== "live" || missingTab ||
    (!hasContinuation(liveFeed) && readUpstream(() =>
      ("has_continuation" in liveFeed && liveFeed.has_continuation === false) ||
      ("hasContinuation" in liveFeed && liveFeed.hasContinuation === false)));
  const pagination = paginationResult({ pageCount: 1, reason: exhausted ? "exhausted" : "max_pages", continuity: "NOT_APPLICABLE" });
  const query = kind === "live" && !missingTab ? {
    channel_id: id, source: "streams", statuses: ["ENDED", "LIVE", "UPCOMING"], exhausted, page_count: 1,
  } : undefined;
  // access_restricted=true가 한 byte 짧으므로 hydration 전에는 안전한 하한만 센다.
  const reserveBytes = encodedSize({ protocol_version: 1, live_sessions: [], profile: {}, photo: [], ...pagination,
    ...(query === undefined ? {} : { live_query: { ...query, access_restricted: true } }) });
  const { sessions: liveSessions, minimumBytes } = collectLiveSnapshot(liveFeed, id, maxSuccessResponseBytes, reserveBytes);
  const unavailable = await enrichUpcomingSchedules(liveSessions, innertube, minimumBytes, maxSuccessResponseBytes);
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

async function enrichUpcomingSchedules(sessions, innertube, minimumBytes, maxSuccessResponseBytes) {
  const candidates = new Map();
  const confirmedIDs = new Set();
  let liveRowCount = 0;
  for (const session of sessions) {
    if (session.status !== "UPCOMING" || session.scheduled_at != null) {
      confirmedIDs.add(session.video_id);
      liveRowCount++;
      continue;
    }
    const rows = candidates.get(session.video_id) ?? [];
    rows.push(session);
    candidates.set(session.video_id, rows);
  }
  if (candidates.size > maxScheduleMetadataLookups) {
    const error = new Error("upcoming schedule metadata lookup limit exceeded");
    error.code = "parser_drift";
    throw error;
  }

  const unavailable = new Map();
  let pendingCount = candidates.size;
  for (const [videoId, rows] of candidates) {
    const metadata = await fetchLiveMetadata(innertube, videoId);
    const identity = { video_id: videoId, channel_id: rows[0].channel_id };
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
        }
        minimumBytes += encodedSize(session);
      }
      minimumBytes += rows.length - 1;
      liveRowCount += rows.length;
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
  return [...unavailable.values()];
}

export function mapLiveSessions(feed, channelId) {
  return Array.from(liveSessionRows(feed, channelId));
}

function* liveSessionRows(feed, channelId) {
  let rows;
  const videos = readUpstream(() => feed?.videos);
  if (Array.isArray(videos)) {
    rows = videos;
  } else if (Array.isArray(readUpstream(() => feed?.items))) {
    rows = readUpstream(() => feed.items);
  } else {
    const error = new Error("live page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
  for (const row of rows) {
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
    const thumbnail = firstThumbnail(row?.thumbnails || row?.thumbnail || row?.content_image);
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
      scheduled_at: optionalTime(row?.scheduled || row?.upcoming),
      started_at: optionalTime(row?.start_time || row?.started),
      ended_at: optionalTime(row?.end_time || row?.ended),
    };
  }
}


export function mapProfile(channel, about) {
  // 최신 about 응답은 AboutChannel.metadata에 필드를 보관합니다.
  const profile = about?.type === "AboutChannel" ? about.metadata : about;
  const providedHandle = profile?.handle ?? channel?.header?.channel_handle ?? channel?.handle;
  let metadataHandle = null;
  if (providedHandle == null && typeof channel?.metadata?.vanity_channel_url === "string") {
    try {
      const url = new URL(channel.metadata.vanity_channel_url);
      if ((url.protocol === "https:" || url.protocol === "http:") &&
          (url.hostname === "www.youtube.com" || url.hostname === "youtube.com") &&
          url.port === "" && url.username === "" && url.password === "") {
        // legacy /user·/c 주소는 handle을 증명하지 않습니다.
        const match = /^\/(@[^\s/]+)\/?$/u.exec(decodeURIComponent(url.pathname));
        metadataHandle = match?.[1] ?? null;
      }
    } catch {
      // 파싱할 수 없는 vanity URL은 handle 값이 없는 상태로 보존합니다.
    }
  }
  return {
    handle: optionalText(providedHandle ?? metadataHandle ?? channel?.vanity_channel_url),
    description: optionalText(profile?.description ?? channel?.metadata?.description ?? channel?.description),
    country: optionalText(profile?.country ?? channel?.country),
    joined_date: optionalText(profile?.joined ?? profile?.joined_date ?? channel?.joined),
  };
}

export function mapPhoto(channel, about) {
  const variants = [];
  const header = channel?.header;
  const headerImage = header?.type === "PageHeader" ? header.content?.image : undefined;
  const avatar = firstThumbnail(headerImage?.type === "DecoratedAvatarView" ? headerImage.avatar?.image : headerImage?.image)
    ?? firstThumbnail(header?.author?.thumbnails)
    ?? firstThumbnail(channel?.metadata?.avatar)
    ?? firstThumbnail(channel?.author?.thumbnails)
    ?? firstThumbnail(about?.avatar);
  if (avatar != null) {
    variants.push({ kind: "avatar", url: avatar.url, width: avatar.width, height: avatar.height });
  }
  const banner = firstThumbnail(header?.type === "PageHeader" ? header.content?.banner?.image : header?.banner)
    ?? firstThumbnail(about?.banner);
  if (banner != null) {
    variants.push({ kind: "banner", url: banner.url, width: banner.width, height: banner.height });
  }
  return variants;
}

function mapLiveStatus(row) {
  if (row?.is_live === true || row?.isLive === true) {
    return "LIVE";
  }
  if (row?.is_upcoming === true || row?.isUpcoming === true) {
    return "UPCOMING";
  }
  const status = textOf(row?.status).toUpperCase();
  if (status === "LIVE" || status === "UPCOMING" || status === "ENDED" || status === "CANCELLED") {
    return status;
  }
  if (isVideoLockup(row)) {
    const badges = lockupBadgeTexts(row);
    if (badges.includes("live")) return "LIVE";
    if (badges.includes("upcoming")) return "UPCOMING";
    return "ENDED";
  }
  return "";
}


function optionalText(value) {
  const text = textOf(value).trim();
  return text === "" ? null : text;
}

function firstThumbnail(value) {
  const rows = thumbnailsOf(value);
  return rows[0] ?? null;
}

function optionalTime(value) {
  if (value instanceof Date) {
    return Number.isFinite(value.getTime()) ? value.toISOString() : undefined;
  }
  if (typeof value !== "string") {
    return undefined;
  }
  const text = value.trim();
  if (!rfc3339Pattern.test(text)) {
    return undefined;
  }
  const parsed = Date.parse(text);
  if (!Number.isFinite(parsed)) {
    return undefined;
  }
  return new Date(parsed).toISOString();
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
