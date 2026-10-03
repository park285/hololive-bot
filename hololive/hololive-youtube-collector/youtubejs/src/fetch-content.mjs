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
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "channel id is required");
  }
  if (contentKind !== "videos" && contentKind !== "shorts") {
    const err = new Error("content kind must be videos or shorts");
    err.code = "parser_drift";
    throw err;
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
  if (Array.isArray(videos)) {
    return videos;
  }
  const items = readUpstream(() => feed?.items);
  if (Array.isArray(items)) {
    return items;
  } else {
    const error = new Error("content page shape is not recognized");
    error.code = "parser_drift";
    throw error;
  }
}

// 예산에 선택되지 않은 항목은 정규화하지 않도록 행을 지연 변환합니다.
function* mapContentPage(feed, channelId, kind) {
  for (const row of contentRows(feed)) {
    yield mapContentRow(row, channelId, kind);
  }
}

// videos 탭 lockup의 공개 시각은 상대 문자열("3 hours ago")이고 예정 시각은 지역화 문자열이라 절대 시각 근거가 아닙니다.
// 그래서 videos 항목에는 시각을 싣지 않고, collector가 영상별 video_live_check RPC(호출마다 limiter 1회)로 근거를 얻습니다.
// is_upcoming은 구조화된 예정 표시일 뿐 시각 근거가 아니며, collector가 저장한 근거가 현재 상태와 맞는지 판단할 때만 씁니다.
// 이 함수는 upstream 호출을 하지 않으므로 목록 RPC 하나가 숨은 영상별 호출을 만들지 않습니다.
function mapContentRow(row, channelId, kind) {
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
  if (kind === "videos") {
    return isUpcomingContentRow(row) ? { ...item, is_upcoming: true } : item;
  }
  return {
    ...item,
    published_at: optionalTime(row?.published || row?.published_at),
    scheduled_for: optionalTime(row?.scheduled || row?.scheduled_for),
  };
}

function isUpcomingContentRow(row) {
  return row?.is_upcoming === true ||
    row?.isUpcoming === true ||
    lockupBadgeTexts(row).includes("upcoming");
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
