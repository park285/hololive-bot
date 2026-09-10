import assert from "node:assert/strict";
import test from "node:test";
import * as mod from "./media.ts";

test("getThumbnailSource normalizes CHZZK template thumbnails", () => {
	const thumbnail = mod.getThumbnailSource(
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_{type}.jpg",
		"max",
	);

	assert.ok(thumbnail);
	assert.equal(
		thumbnail.src,
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_720.jpg",
	);
	assert.deepEqual(thumbnail.fallbackChain, [
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_480.jpg",
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_360.jpg",
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_270.jpg",
		"https://livecloud-thumb.akamaized.net/chzzk/livecloud/KR/stream/123/live/456/record/789/thumbnail/image_144.jpg",
	]);
});

test("getStreamLinkMeta uses CHZZK label for CHZZK streams", () => {
	const linkMeta = mod.getStreamLinkMeta({
		id: "",
		title: "치지직 방송",
		status: "live",
		channel_id: "yt-1",
		link: "https://chzzk.naver.com/live/chzzk-channel",
	});

	assert.equal(linkMeta.href, "https://chzzk.naver.com/live/chzzk-channel");
	assert.equal(linkMeta.label, "Watch on CHZZK");
	assert.equal(linkMeta.badge, "CHZZK");
});

test("getStreamLinkMeta falls back to YouTube watch URL for YouTube streams", () => {
	const linkMeta = mod.getStreamLinkMeta({
		id: "abc123",
		title: "youtube live",
		status: "live",
		channel_id: "yt-1",
	});

	assert.equal(linkMeta.href, "https://www.youtube.com/watch?v=abc123");
	assert.equal(linkMeta.label, "Watch on YouTube");
	assert.equal(linkMeta.badge, "YouTube");
});

test("unsafe URL schemes and credential URLs never become image or navigation targets", () => {
 for (const link of ["javascript:alert(1)", "data:text/html,unsafe", "file:///etc/passwd", "https://user:password@example.test/path"]) {
  assert.equal(mod.getStreamLinkMeta({ id: "fixture", link }).href, undefined);
  assert.equal(mod.getThumbnailSource(link), undefined);
 }
 assert.equal(mod.getStreamLinkMeta({ id: "fixture", link: "https://example.test/chzzk.naver.com" }).badge, "Link");
 const escaped = mod.getStreamLinkMeta({ id: "id&other=value#fragment" }).href;
 assert.equal(new URL(escaped).searchParams.get("v"), "id&other=value#fragment");
 assert.equal(new URL(escaped).searchParams.has("other"), false);
});
