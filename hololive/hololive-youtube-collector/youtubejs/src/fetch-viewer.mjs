import { textOf } from "./map-posts.mjs";

/** @param {YouTubeJSFetchOptions} [options] */
export async function fetchViewerFeed({ videoId, innertube } = {}) {
  const id = String(videoId ?? "").trim();
  if (id === "") {
    throw new Error("video id is required");
  }
  if (innertube == null || typeof innertube.getInfo !== "function") {
    throw new Error("innertube client is required");
  }
  const info = await innertube.getInfo(id);
  return mapViewer(info, id);
}

export function mapViewer(info, videoId) {
  const basic = info?.basic_info ?? info?.basicInfo ?? {};
  const streaming = info?.streaming_data ?? info?.streamingData ?? {};
  const isLive = basic.is_live === true || basic.isLive === true;
  if (!isLive) {
    return {
      video_id: textOf(basic.id || videoId).trim() || videoId,
      viewer_count: null,
      availability: "UNAVAILABLE",
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
    };
  }
  const raw =
    basic.view_count ??
    basic.viewCount ??
    streaming.view_count ??
    info?.view_count ??
    info?.viewer_count;
  const hidden = basic.view_count_is_live === false && raw == null
    ? true
    : basic.hidden_view_count === true || info?.hidden_view_count === true;
  if (hidden || raw == null || raw === "") {
    return {
      video_id: videoId,
      viewer_count: null,
      availability: hidden ? "HIDDEN" : "UNAVAILABLE",
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
    };
  }
  const count = Number(raw);
  if (!Number.isFinite(count) || count < 0) {
    const err = new Error("viewer count schema drifted");
    err.code = "parser_drift";
    throw err;
  }
  return {
    video_id: textOf(basic.id || videoId).trim() || videoId,
    viewer_count: Math.trunc(count),
    availability: "AVAILABLE",
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
}
