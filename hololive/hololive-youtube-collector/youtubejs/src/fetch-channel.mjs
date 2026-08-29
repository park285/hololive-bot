import { Utils } from "youtubei.js";

import { textOf, thumbnailsOf } from "./map-posts.mjs";
import { isVideoLockup, lockupBadgeTexts, videoIDOf, videoTitleOf } from "./map-lockup.mjs";
import { assertResponseBudget, encodedSize, paginationResult } from "./pagination.mjs";

const responseReserveBytes = encodedSize({
  protocol_version: 1,
  live_sessions: [],
  stats: {},
  profile: {},
  photo: [],
  ...paginationResult({
    pageCount: 1,
    reason: "exhausted",
    continuity: "NOT_APPLICABLE",
  }),
});

/** @param {YouTubeJSFetchOptions} [options] */
export async function fetchChannelFeed({
  channelId,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
} = {}) {
  const id = String(channelId ?? "").trim();
  if (id === "") {
    throw new Error("channel id is required");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new Error("innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const channel = await innertube.getChannel(id);
  const about = typeof channel.getAbout === "function" ? await channel.getAbout() : {};
  let liveFeed = { videos: [] };
  let missingTab = false;
  if (typeof channel.getLiveStreams === "function") {
    try {
      liveFeed = await channel.getLiveStreams();
    } catch (err) {
      if (!isMissingStreamsTab(err)) {
        throw err;
      }
      missingTab = true;
    }
  } else {
    missingTab = true;
  }
  return {
    live_sessions: mapLiveSessions(liveFeed, id),
    stats: mapStats(channel, about),
    profile: mapProfile(channel, about),
    photo: mapPhoto(channel, about),
    ...paginationResult({
      pageCount: 1,
      reason: "exhausted",
      continuity: "NOT_APPLICABLE",
    }),
    ...(missingTab ? { missing_tab: true } : {}),
  };
}

function isMissingStreamsTab(err) {
  return err instanceof Utils.InnertubeError && err.message === 'Tab "streams" not found';
}

export function mapLiveSessions(feed, channelId) {
  let rows;
  if (Array.isArray(feed?.videos)) {
    rows = feed.videos;
  } else if (Array.isArray(feed?.items)) {
    rows = feed.items;
  } else {
    const error = new Error("live page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
  const sessions = [];
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
    sessions.push({
      video_id: videoId,
      channel_id: textOf(row?.author?.id || channelId).trim() || channelId,
      status,
      ...(title === "" ? {} : { title }),
      ...(thumbnailURL === "" ? {} : { thumbnail_url: thumbnailURL }),
      scheduled_at: optionalTime(row?.scheduled || row?.upcoming),
      started_at: optionalTime(row?.start_time || row?.started),
      ended_at: optionalTime(row?.end_time || row?.ended),
    });
  }
  return sessions;
}

export function mapStats(channel, about) {
  return {
    subscriber_count: optionalCount(about?.subscriber_count ?? channel?.subscriber_count ?? channel?.subscribers),
    view_count: optionalCount(about?.view_count ?? channel?.view_count),
    video_count: optionalCount(about?.video_count ?? channel?.video_count),
  };
}

export function mapProfile(channel, about) {
  return {
    handle: optionalText(about?.handle ?? channel?.handle ?? channel?.vanity_channel_url),
    description: optionalText(about?.description ?? channel?.description),
    country: optionalText(about?.country ?? channel?.country),
    joined_date: optionalText(about?.joined ?? about?.joined_date ?? channel?.joined),
  };
}

export function mapPhoto(channel, about) {
  const variants = [];
  const avatar = firstThumbnail(channel?.header?.author?.thumbnails || channel?.author?.thumbnails || about?.avatar);
  if (avatar != null) {
    variants.push({ kind: "avatar", url: avatar.url, width: avatar.width, height: avatar.height });
  }
  const banner = firstThumbnail(channel?.header?.banner?.thumbnails || about?.banner);
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

function optionalCount(value) {
  if (value == null || value === "") {
    return null;
  }
  if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
    return Math.trunc(value);
  }
  const parsed = Number.parseInt(String(value).replaceAll(",", ""), 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return null;
  }
  return parsed;
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
  const text = textOf(value).trim();
  if (text === "") {
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
