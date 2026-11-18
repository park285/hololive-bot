import { textOf } from "./map-posts.mjs";

const liveBadgeStyle = "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE";
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

export function assertVideoRowTitled(row) {
  if (row?.type === "LockupView" && row.metadata?.title === undefined) {
    throw Object.assign(new Error("video row has no title"), { code: "parser_drift" });
  }
}

export function hasLiveMarker(row) {
  const badges = Array.isArray(row?.badges) ? row.badges : [];
  return badges.some((badge) => badge?.style === "BADGE_STYLE_TYPE_LIVE_NOW" || badge?.label === "LIVE") ||
    timeStatusOverlays(row).some((overlay) => overlay.style === "LIVE");
}

export function hasUpcomingMarker(row) {
  return row?.upcoming instanceof Date || timeStatusOverlays(row).some((overlay) => overlay.style === "UPCOMING");
}

export function hasDurationMarker(row) {
  return timeStatusOverlays(row).some((overlay) => overlay.style === "DEFAULT" && durationText.test(textOf(overlay.text).trim()));
}

function timeStatusOverlays(row) {
  const overlays = Array.isArray(row?.thumbnail_overlays) ? row.thumbnail_overlays : [];
  return overlays.filter((overlay) => overlay?.type === "ThumbnailOverlayTimeStatus");
}

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

export function videoThumbnailsOf(row) {
  return row?.thumbnails ?? row?.thumbnail ?? lockupThumbnailView(row)?.image;
}

function lockupBadges(row) {
  const overlays = lockupThumbnailView(row)?.overlays;
  return Array.isArray(overlays) ? overlays.flatMap((overlay) => (Array.isArray(overlay?.badges) ? overlay.badges : [])) : [];
}

function lockupThumbnailView(row) {
  const image = row?.content_image;
  return image?.type === "CollectionThumbnailView" ? image.primary_thumbnail : image;
}
