import { textOf } from "./map-posts.mjs";

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

export function lockupBadgeTexts(row) {
  const overlays = Array.isArray(row?.content_image?.overlays) ? row.content_image.overlays : [];
  return overlays
    .flatMap((overlay) => (Array.isArray(overlay?.badges) ? overlay.badges : []))
    .map((badge) => textOf(badge?.text).trim().toLowerCase())
    .filter(Boolean);
}
