import {
  assertResponseBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";
import { textOf } from "./map-posts.mjs";
import { videoIDOf, videoTitleOf } from "./map-lockup.mjs";

const responseReserveBytes = paginationEnvelopeReserve({ protocol_version: 1, items: [] });

/** @param {YouTubeJSFetchOptions} [options] */
export async function fetchContentFeed({
  channelId,
  kind,
  maxResults,
  maxPages,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
} = {}) {
  const id = String(channelId ?? "").trim();
  const contentKind = String(kind ?? "").trim();
  if (id === "") {
    throw new Error("channel id is required");
  }
  if (contentKind !== "videos" && contentKind !== "shorts") {
    const err = new Error("content kind must be videos or shorts");
    err.code = "parser_drift";
    throw err;
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new Error("innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const channel = await innertube.getChannel(id);
  const loader = contentKind === "shorts" ? channel.getShorts : channel.getVideos;
  if (typeof loader !== "function") {
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
  const feed = await loader.call(channel);
  const paged = await paginate({
    firstPage: feed,
    getContinuation: async (current) => {
      if (typeof current.getContinuation !== "function") {
        const err = new Error("content continuation is missing");
        err.code = "parser_drift";
        throw err;
      }
      return current.getContinuation();
    },
    mapPage: (current) => ({ recognized_shape: true, items: mapContentItems(current, id) }),
    maxPages,
    maxResults,
    maxSuccessResponseBytes,
    reservedEnvelopeBytes: responseReserveBytes,
    buildResult: (items, pagination) => ({ items, ...pagination }),
  });
  return paged;
}

export function mapContentItems(feed, channelId) {
  let rows;
  if (Array.isArray(feed?.videos)) {
    rows = feed.videos;
  } else if (Array.isArray(feed?.items)) {
    rows = feed.items;
  } else {
    const error = new Error("content page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
  const mapped = [];
  for (const row of rows) {
    const videoId = videoIDOf(row);
    if (videoId === "") {
      const err = new Error("content row is missing video id");
      err.code = "parser_drift";
      throw err;
    }
    mapped.push({
      video_id: videoId,
      channel_id: textOf(row?.author?.id || row?.channel_id || channelId).trim() || channelId,
      title: videoTitleOf(row),
      published_at: optionalTime(row?.published || row?.published_at),
      scheduled_for: optionalTime(row?.scheduled || row?.scheduled_for),
    });
  }
  return mapped;
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
