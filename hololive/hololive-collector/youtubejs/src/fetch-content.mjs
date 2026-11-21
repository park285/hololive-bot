import { FetchTransportError } from "./fetch-transport.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";
import { fetchChannelTab } from "./channel-tabs.mjs";
import {
  assertResponseBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";
import { textOf } from "./map-posts.mjs";
import { assertVideoRowTitled, hasUpcomingMarker, lockupBadgeTexts, videoIDOf, videoTitleOf } from "./map-lockup.mjs";

const responseReserveBytes = paginationEnvelopeReserve({ protocol_version: 1, items: [] });

/**
 * @param {YouTubeJSFetchOptions} [options]
 * @returns {Promise<Omit<import("./contracts.d.ts").ContentResult, "protocol_version">>}
 */
export async function fetchContentFeed({
  channelId: id,
  kind: contentKind,
  maxResults,
  maxPages,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
} = {}) {
  if (contentKind !== "videos" && contentKind !== "shorts") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "content kind must be videos or shorts");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const channel = await runUpstream(() => innertube.getChannel(id));
  const loader = contentKind === "shorts" ? channel.getShorts : channel.getVideos;
  if (typeof loader !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "content tab loader is unavailable");
  }
  const tab = await fetchChannelTab(channel, contentKind, () => loader.call(channel));
  if (tab.missing === true) {
    return {
      items: [],
      ...paginationResult({
        pageCount: 1,
        reason: "exhausted",
        continuity: "NOT_APPLICABLE",
      }),
      missing_tab: true,
    };
  }
  const paged = await paginate({
    firstPage: tab.feed,
    getContinuation: async (current) => {
      if (typeof current.getContinuation !== "function") {
        const err = new Error("content continuation is missing");
        err.code = "parser_drift";
        throw err;
      }
      return runUpstream(() => current.getContinuation());
    },
    mapPage: async (current) => ({
      recognized_shape: true,
      items: mapContentPage(current, id, contentKind),
    }),
    maxPages,
    maxResults,
    maxSuccessResponseBytes,
    reservedEnvelopeBytes: responseReserveBytes,
    buildResult: (items, pagination) => ({ items, ...pagination }),
  });
  return paged;
}

export function mapContentItems(feed, channelId, kind = "shorts") {
  return Array.from(contentRows(feed), (row) => mapContentRow(row, channelId, kind));
}

function contentRows(feed) {
  const videos = readUpstream(() => feed?.videos);
  if (!Array.isArray(videos)) {
    const error = new Error("content page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
  return videos;
}

function* mapContentPage(feed, channelId, kind) {
  for (const row of contentRows(feed)) {
    yield mapContentRow(row, channelId, kind);
  }
}

function mapContentRow(row, channelId, kind) {
  assertVideoRowTitled(row);
  const videoId = videoIDOf(row);
  if (videoId === "") {
    const err = new Error("content row is missing video id");
    err.code = "parser_drift";
    throw err;
  }
  const item = {
    video_id: videoId,
    channel_id: textOf(row?.author?.id || row?.channel_id || channelId).trim() || channelId,
    title: videoTitleOf(row),
  };
  if (kind === "videos" && (hasUpcomingMarker(row) || lockupBadgeTexts(row).includes("upcoming"))) {
    return { ...item, is_upcoming: true };
  }
  return item;
}
