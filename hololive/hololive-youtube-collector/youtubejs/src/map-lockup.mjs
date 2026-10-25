import { textOf } from "./map-posts.mjs";

// 모든 상태 판정은 원문의 구조화된 표시만 씁니다. SDK의 is_live·is_upcoming getter는 버전·현재 시각에 따라
// 바뀌는 추정 규칙입니다(18.2부터 time-status 표시가 없으면 is_live가 true, is_upcoming은 시작 시각이 미래일 때만 true).
const liveBadgeStyle = "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE";
// 재생 시간 배지는 종료된 방송의 유일한 구조화된 표시입니다. SDK LockupView.duration과 같은 형식입니다.
const durationText = /^\d{1,3}(?:,\d{3})*:\d{2}(?::\d{2})?$/u;

export function videoIDOf(row) {
  return textOf(
    row?.id ||
    row?.video_id ||
    row?.videoId ||
    row?.content_id ||
    row?.on_tap_endpoint?.payload?.videoId,
  ).trim();
}

export function videoTitleOf(row) {
  return textOf(row?.title || row?.metadata?.title || row?.overlay_metadata?.primary_text).trim();
}

export function isVideoLockup(row) {
  return row?.type === "LockupView" && row?.content_type === "VIDEO";
}

// 제목 노드가 없는 lockup은 영상 행이 아닙니다. SDK 18.2부터 오류 대신 빈 제목으로 통과하므로 여기서 거부합니다.
export function assertVideoRowTitled(row) {
  if (row?.type === "LockupView" && row.metadata?.title === undefined) {
    throw Object.assign(new Error("video row has no title"), { code: "parser_drift" });
  }
}

// Video(videoRenderer) 행의 LIVE 표시: LIVE_NOW 배지, LIVE 라벨, LIVE time-status overlay.
export function hasLiveMarker(row) {
  const badges = Array.isArray(row?.badges) ? row.badges : [];
  return badges.some((badge) => badge?.style === "BADGE_STYLE_TYPE_LIVE_NOW" || badge?.label === "LIVE") ||
    timeStatusOverlays(row).some((overlay) => overlay.style === "LIVE");
}

// Video 행의 예정 표시: upcomingEventData 시각(지난 예정도 포함)과 UPCOMING time-status overlay.
export function hasUpcomingMarker(row) {
  return row?.upcoming instanceof Date || timeStatusOverlays(row).some((overlay) => overlay.style === "UPCOMING");
}

// Video 행의 종료 표시: 재생 시간 time-status overlay.
export function hasDurationMarker(row) {
  return timeStatusOverlays(row).some((overlay) => overlay.style === "DEFAULT" && durationText.test(textOf(overlay.text).trim()));
}

function timeStatusOverlays(row) {
  const overlays = Array.isArray(row?.thumbnail_overlays) ? row.thumbnail_overlays : [];
  return overlays.filter((overlay) => overlay?.type === "ThumbnailOverlayTimeStatus");
}

// LockupView 행의 상태. 세션은 generate_session_locally로 hl=en이 고정되어 배지 문자열 "Upcoming"은 지역화되지
// 않습니다. 표시가 하나도 없으면 상태를 추정하지 않고 ""를 돌려 호출자가 drift로 거부하게 합니다.
export function lockupLiveStatus(row) {
  const badges = lockupBadges(row);
  if (badges.some((badge) => badge?.badge_style === liveBadgeStyle || textOf(badge?.text).trim().toLowerCase() === "live")) {
    return "LIVE";
  }
  const texts = badges.map((badge) => textOf(badge?.text).trim());
  if (texts.some((text) => text.toLowerCase() === "upcoming")) return "UPCOMING";
  if (texts.some((text) => durationText.test(text))) return "ENDED";
  return "";
}

export function lockupBadgeTexts(row) {
  return lockupBadges(row)
    .map((badge) => textOf(badge?.text).trim().toLowerCase())
    .filter(Boolean);
}

function lockupBadges(row) {
  const overlays = Array.isArray(row?.content_image?.overlays) ? row.content_image.overlays : [];
  return overlays.flatMap((overlay) => (Array.isArray(overlay?.badges) ? overlay.badges : []));
}
