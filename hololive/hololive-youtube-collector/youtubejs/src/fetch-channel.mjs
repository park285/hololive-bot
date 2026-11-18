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
  cache?.beginSlot(id, slotAt);
  const channel = await runUpstream(() => innertube.getChannel(id));
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
  const reserveBytes = encodedSize({ protocol_version: 1, live_sessions: [], profile: {}, photo: [], ...pagination,
    ...(query === undefined ? {} : { schedule_lookups: 0, live_query: { ...query, access_restricted: true } }) });
  const { sessions: liveSessions, minimumBytes } = collectLiveSnapshot(liveFeed, id, maxSuccessResponseBytes, reserveBytes);
  const { unavailable, lookups } = await enrichUpcomingSchedules(
    liveSessions, innertube, minimumBytes, maxSuccessResponseBytes, maxScheduleLookups, cache, slotAt);
  const unavailableIDs = new Set(unavailable.map((item) => item.video_id));
  if (unavailable.length > 0) {
    logUnavailableSchedules(id, unavailable);
  }
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
 * @param {import("./schedule-cache.mjs").ScheduleLookupCache | undefined} cache
 * @param {number} slotAt
 */
async function enrichUpcomingSchedules(sessions, innertube, minimumBytes, maxSuccessResponseBytes, maxLookups, cache, slotAt) {
  const candidates = new Map();
  const confirmedIDs = new Set();
  let liveRowCount = 0;
  for (const session of sessions) {
    if (session.status !== "UPCOMING" || session.scheduled_at != null) {
      confirmedIDs.add(session.video_id);
      cache?.forget(session.channel_id, session.video_id, slotAt);
      liveRowCount++;
      continue;
    }
    const rows = candidates.get(session.video_id) ?? [];
    rows.push(session);
    candidates.set(session.video_id, rows);
  }
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
          if (reused !== undefined) {
            session.schedule_observed_at = reused.observedAt;
          }
        }
        minimumBytes += encodedSize(session);
      }
      minimumBytes += rows.length - 1;
      liveRowCount += rows.length;
    }
    if (reused === undefined) {
      cache?.remember(channelId, videoId, metadata, slotAt);
    }
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
  const profile = about?.type === "AboutChannel" ? about.metadata : about;
  const providedHandle = channel?.header?.channel_handle;
  let metadataHandle = null;
  if (providedHandle == null && typeof channel?.metadata?.vanity_channel_url === "string") {
    try {
      const url = new URL(channel.metadata.vanity_channel_url);
      if ((url.protocol === "https:" || url.protocol === "http:") &&
          (url.hostname === "www.youtube.com" || url.hostname === "youtube.com") &&
          url.port === "" && url.username === "" && url.password === "") {
        const match = vanityHandlePathPattern.exec(decodeURIComponent(url.pathname));
        metadataHandle = match?.[1] ?? null;
      }
    } catch {
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
      if (unresolvedIDs.has(session.video_id)) {
        minimum = null;
      } else {
        unresolvedIDs.add(session.video_id);
        minimum = { video_id: session.video_id, channel_id: session.channel_id };
      }
    }
    if (minimum != null && budget.tryAppend(minimum) === "WOULD_EXCEED") {
      const error = new Error("live snapshot minimum exceeds success response limit");
      error.code = "response_too_large";
      throw error;
    }
    sessions.push(session);
  }
  return { sessions, minimumBytes: reserveBytes + budget.encodedItemsBytes() };
}
