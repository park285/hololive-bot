import {
  assertResponseBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";
import { textOf } from "./map-posts.mjs";
import { lockupBadgeTexts, videoIDOf, videoTitleOf } from "./map-lockup.mjs";

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
    mapPage: async (current) => ({
      recognized_shape: true,
      items: contentKind === "videos"
        ? await mapContentPage(current, id, innertube)
        : mapContentItems(current, id),
    }),
    maxPages,
    maxResults,
    maxSuccessResponseBytes,
    reservedEnvelopeBytes: responseReserveBytes,
    buildResult: (items, pagination) => ({ items, ...pagination }),
  });
  return paged;
}

export function mapContentItems(feed, channelId) {
  return mapContentRows(contentRows(feed), channelId);
}

function contentRows(feed) {
  if (Array.isArray(feed?.videos)) {
    return feed.videos;
  } else if (Array.isArray(feed?.items)) {
    return feed.items;
  } else {
    const error = new Error("content page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
}

function mapContentRows(rows, channelId) {
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

async function mapContentPage(feed, channelId, innertube) {
  const rows = contentRows(feed);
  const items = mapContentRows(rows, channelId);

  for (let index = 0; index < rows.length; index += 1) {
    if (!isUpcomingContentRow(rows[index])) {
      continue;
    }
    if (typeof innertube.getInfo !== "function") {
      const error = new Error("upcoming content metadata lookup is unavailable");
      error.code = "parser_drift";
      throw error;
    }

    const info = await innertube.getInfo(items[index].video_id);
    const premiere = premiereMetadata(info);
    if (premiere != null) {
      items[index].is_premiere = true;
      if (premiere.scheduledFor != null) {
        items[index].scheduled_for = premiere.scheduledFor;
      }
    }
  }

  return items;
}

function isUpcomingContentRow(row) {
  return row?.is_upcoming === true ||
    row?.isUpcoming === true ||
    lockupBadgeTexts(row).includes("upcoming");
}

function premiereMetadata(info) {
  const basic = info?.basic_info ?? info?.basicInfo ?? {};
  const isUpcoming = basic.is_upcoming === true || basic.isUpcoming === true;
  const isLiveContent = basic.is_live_content ?? basic.isLiveContent;
  if (!isUpcoming || isLiveContent !== false) {
    return undefined;
  }

  const scheduledFor = optionalTime(basic.start_timestamp ?? basic.startTimestamp);
  return { scheduledFor };
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
