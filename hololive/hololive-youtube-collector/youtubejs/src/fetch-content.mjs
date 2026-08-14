import { paginate } from "./pagination.mjs";
import { textOf } from "./map-posts.mjs";

export async function fetchContentFeed({
  channelId,
  kind,
  maxResults,
  maxPages,
  maxAggregateBytes,
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
  const channel = await innertube.getChannel(id);
  const loader = contentKind === "shorts" ? channel.getShorts : channel.getVideos;
  if (typeof loader !== "function") {
    return {
      items: [],
      page_count: 0,
      exhausted: false,
      continuity: "GAP_UNRESOLVED",
      missing_tab: true,
    };
  }
  const feed = await loader.call(channel);
  const paged = await paginate({
    firstPage: feed,
    getContinuation: async (current) => {
      if (typeof current.getContinuation !== "function") {
        const err = new Error("content continuation is missing");
        err.code = "pagination_gap";
        throw err;
      }
      return current.getContinuation();
    },
    mapPage: (current) => mapContentItems(current, id),
    maxPages,
    maxResults,
    maxAggregateBytes,
  });
  return {
    items: paged.items,
    page_count: paged.page_count,
    cursor_start: paged.cursor_start,
    cursor_end: paged.cursor_end,
    exhausted: paged.exhausted,
    continuity: paged.continuity,
  };
}

export function mapContentItems(feed, channelId) {
  const rows = Array.isArray(feed?.videos)
    ? feed.videos
    : Array.isArray(feed?.items)
      ? feed.items
      : [];
    const mapped = [];
  for (const row of rows) {
    const videoId = textOf(row?.id || row?.video_id || row?.videoId).trim();
    if (videoId === "") {
      const err = new Error("content row is missing video id");
      err.code = "parser_drift";
      throw err;
    }
    mapped.push({
      video_id: videoId,
      channel_id: textOf(row?.author?.id || row?.channel_id || channelId).trim() || channelId,
      title: textOf(row?.title),
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
