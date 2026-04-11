import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const sourcePath = new URL(
	"./media.ts",
	import.meta.url,
);
const source = await readFile(sourcePath, "utf8");
const transpiled = ts.transpileModule(source, {
	compilerOptions: {
		module: ts.ModuleKind.ESNext,
		target: ts.ScriptTarget.ES2022,
	},
});

const mod = await import(
	`data:text/javascript;base64,${Buffer.from(transpiled.outputText).toString("base64")}`
);

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
