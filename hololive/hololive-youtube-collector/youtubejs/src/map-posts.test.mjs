import assert from "node:assert/strict";
import test from "node:test";

import { mapPost, mapPosts, parseShortNumber, textOf, thumbnailsOf } from "./map-posts.mjs";

test("textOf reads YouTube.js Text-like objects", () => {
  assert.equal(textOf({ toString: () => "hello" }), "hello");
  assert.equal(textOf({ text: "from-text" }), "from-text");
  assert.equal(textOf(""), "");
});

test("parseShortNumber matches the Go scraper compact counts", () => {
  assert.equal(parseShortNumber("1.2K"), 1200);
  assert.equal(parseShortNumber("7"), 7);
  assert.equal(parseShortNumber(""), 0);
});

test("thumbnailsOf keeps https urls", () => {
  assert.deepEqual(
    thumbnailsOf([{ url: "https://img.test/a.jpg", width: 88, height: 88 }]),
    [{ url: "https://img.test/a.jpg", width: 88, height: 88 }],
  );
});

test("thumbnailsOf makes protocol-relative YouTube urls absolute", () => {
  assert.deepEqual(
    thumbnailsOf([{ url: "//yt3.googleusercontent.com/a.jpg", width: 88, height: 88 }]),
    [{ url: "https://yt3.googleusercontent.com/a.jpg", width: 88, height: 88 }],
  );
});

test("mapPost copies BackstagePost fields into the Go CommunityPost wire shape", () => {
  const mapped = mapPost({
    id: "post-1",
    author: {
      id: "UC_TEST",
      name: "Author",
      thumbnails: [{ url: "https://img.test/a.jpg", width: 88, height: 88 }],
    },
    content: { toString: () => "hello world" },
    published: { toString: () => "3 hours ago" },
    vote_count: { toString: () => "1.2K" },
    action_buttons: { reply_button: { text: "7" } },
    attachment: {
      type: "BackstageImage",
      image: [{ url: "https://img.test/p.jpg", width: 640, height: 360 }],
    },
  });
  assert.deepEqual(mapped, {
    postId: "post-1",
    upstreamPostId: "post-1",
    authorId: "UC_TEST",
    authorName: "Author",
    authorPhoto: [{ url: "https://img.test/a.jpg", width: 88, height: 88 }],
    contentText: "hello world",
    publishedText: "3 hours ago",
    likeCount: 1200,
    commentCount: 7,
    images: [{ url: "https://img.test/p.jpg", width: 640, height: 360 }],
    videoId: "",
  });
});

test("mapPost reads attached video ids", () => {
  const mapped = mapPost({
    id: "post-2",
    author: { id: "UC_TEST", name: "Author", thumbnails: [] },
    content: "clip",
    published: "now",
    attachment: { type: "Video", id: "vid-1" },
  });
  assert.equal(mapped.videoId, "vid-1");
});

test("mapPost drops posts without ids", () => {
  assert.equal(mapPost({ content: "no id" }), null);
});

test("mapPosts stops at maxResults", () => {
  const mapped = mapPosts([{ id: "a" }, { id: "b" }, { id: "c" }], 2);
  assert.deepEqual(
    mapped.map((post) => post.postId),
    ["a", "b"],
  );
});
