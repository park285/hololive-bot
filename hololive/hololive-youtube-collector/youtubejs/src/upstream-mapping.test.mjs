import assert from "node:assert/strict";
import test from "node:test";
import { Mixins, Parser, YTNodes } from "youtubei.js";

import { fetchChannelFeed, mapPhoto, mapProfile } from "./fetch-channel.mjs";
import { mapPost } from "./map-posts.mjs";
import { continuationToken, paginate, paginationEnvelopeReserve } from "./pagination.mjs";

const channelId = "UC_TEST";

function modernHeader() {
  return new YTNodes.PageHeader({
    pageTitle: "channel",
    content: { pageHeaderViewModel: {
      image: { decoratedAvatarViewModel: { avatar: { avatarViewModel: {
        image: { sources: [{ url: "https://img.test/avatar.jpg", width: 128, height: 128 }] },
      } } } },
      banner: { imageBannerViewModel: {
        image: { sources: [{ url: "https://img.test/banner.jpg", width: 1024, height: 256 }] },
      } },
    } },
  });
}

function modernAbout(values = {}) {
  return new YTNodes.AboutChannel({ metadata: { aboutChannelViewModel: values } });
}

test("real AboutChannel metadata reaches the profile wire fields", () => {
  const about = modernAbout({
    description: "present description", country: "JP", joinedDateText: { content: "Joined Jan 1, 2020" },
  });
  assert.equal(about.metadata.description, "present description");
  assert.deepEqual(mapProfile({}, about), {
    handle: null, description: "present description", country: "JP", joined_date: "Joined Jan 1, 2020",
  });
});

test("real ChannelAboutFullMetadata retains its direct Text fields", () => {
  const about = new YTNodes.ChannelAboutFullMetadata({
    channelId, description: { simpleText: "legacy description" }, country: { simpleText: "JP" },
    joinedDateText: { simpleText: "Joined Jan 1, 2020" },
  });
  assert.deepEqual(mapProfile({}, about), {
    handle: null, description: "legacy description", country: "JP", joined_date: "Joined Jan 1, 2020",
  });
});

test("real C4 handle and ChannelMetadata description are available", () => {
  const channel = {
    header: new YTNodes.C4TabbedHeader({ title: "channel", channelHandleText: { simpleText: "@actual_handle" } }),
    metadata: new YTNodes.ChannelMetadata({ description: "fallback description", vanityChannelUrl: "https://www.youtube.com/@actual_handle" }),
  };
  assert.equal(mapProfile(channel, {}).handle, "@actual_handle");
  assert.equal(mapProfile(channel, {}).description, "fallback description");
  assert.equal(mapProfile({ metadata: channel.metadata }, {}).handle, "@actual_handle");
});

test("real ChannelMetadata vanity URLs distinguish handles from legacy channel paths", () => {
  for (const [url, expected] of [
    ["https://www.youtube.com/@actual_handle", "@actual_handle"],
    ["http://youtube.com/@actual_handle/", "@actual_handle"],
    ["https://www.youtube.com/@%E6%97%A5%E6%9C%AC%E8%AA%9E", "@日本語"],
    ["http://www.youtube.com/user/legacy_name", null],
    ["https://www.youtube.com/c/legacy_name", null],
    ["https://www.youtube.com/@name/extra", null],
    ["https://www.youtube.com/@name%2Fextra", null],
    ["https://img.test/@name", null],
    ["https://user:pass@www.youtube.com/@name", null],
    ["https://www.youtube.com/@%", null],
    ["not-a-url", null],
  ]) {
    const metadata = new YTNodes.ChannelMetadata({ vanityChannelUrl: url });
    assert.equal(mapProfile({ metadata }, {}).handle, expected, url);
    assert.equal(mapProfile({ metadata }, { handle: "@provided" }).handle, "@provided", url);
  }
});

test("absent and empty profile fields keep the existing null representation", () => {
  const about = modernAbout({ description: "", country: "" });
  assert.deepEqual(mapProfile({}, about), { handle: null, description: null, country: null, joined_date: null });
  assert.deepEqual(mapPhoto({}, about), []);
});

test("real C4 header thumbnail arrays include avatar and banner", () => {
  const header = new YTNodes.C4TabbedHeader({
    title: "channel", avatar: { thumbnails: [{ url: "https://img.test/avatar.jpg", width: 128, height: 128 }] },
    banner: { thumbnails: [{ url: "https://img.test/banner.jpg", width: 1024, height: 256 }] },
  });
  assert.deepEqual(mapPhoto({ header }, {}), [
    { kind: "avatar", url: "https://img.test/avatar.jpg", width: 128, height: 128 },
    { kind: "banner", url: "https://img.test/banner.jpg", width: 1024, height: 256 },
  ]);
});

test("real ChannelMetadata avatar remains available when the header has no image", () => {
  const channel = {
    header: new YTNodes.C4TabbedHeader({ title: "channel" }),
    metadata: new YTNodes.ChannelMetadata({
      avatar: { thumbnails: [{ url: "https://img.test/avatar.jpg", width: 128, height: 128 }] },
    }),
  };
  assert.deepEqual(mapPhoto(channel, {}), [{ kind: "avatar", url: "https://img.test/avatar.jpg", width: 128, height: 128 }]);
});

test("real PageHeader DecoratedAvatarView and ImageBannerView include both photos", () => {
  assert.deepEqual(mapPhoto({ header: modernHeader() }, {}), [
    { kind: "avatar", url: "https://img.test/avatar.jpg", width: 128, height: 128 },
    { kind: "banner", url: "https://img.test/banner.jpg", width: 1024, height: 256 },
  ]);
});

test("real PageHeader ContentPreviewImageView exposes its image array", () => {
  const header = new YTNodes.PageHeader({ content: { pageHeaderViewModel: {
    image: { contentPreviewImageViewModel: { image: { sources: [{ url: "https://img.test/avatar.jpg", width: 128, height: 128 }] } } },
  } } });
  assert.deepEqual(mapPhoto({ header }, {}), [{ kind: "avatar", url: "https://img.test/avatar.jpg", width: 128, height: 128 }]);
});

test("metadata collection preserves real profile and photo classes through its response", async () => {
  const about = modernAbout({ description: "present description", country: "JP" });
  const result = await fetchChannelFeed({ channelId, kind: "metadata", innertube: {
    getChannel: async () => ({ header: modernHeader(), getAbout: async () => about }),
  } });
  assert.equal(result.profile.description, "present description");
  assert.equal(result.profile.country, "JP");
  assert.deepEqual(result.photo.map(variant => variant.kind), ["avatar", "banner"]);
  assert.deepEqual(result.live_sessions, []);
});

test("real multiimage community attachments preserve each image URL and dimensions", () => {
  const post = new YTNodes.BackstagePost({ postId: "post-1", backstageAttachment: { postMultiImageRenderer: {
    images: ["first", "second"].map(id => ({ backstageImageRenderer: { image: {
      thumbnails: [{ url: `https://img.test/${id}.jpg`, width: 640, height: 480 }],
    } } })),
  } } });
  assert.equal(post.attachment.type, "PostMultiImage");
  assert.deepEqual(mapPost(post).images, [
    { url: "https://img.test/first.jpg", width: 640, height: 480 },
    { url: "https://img.test/second.jpg", width: 640, height: 480 },
  ]);
});

test("real single-image attachments preserve their existing representation", () => {
  const post = new YTNodes.BackstagePost({ postId: "post-1", backstageAttachment: { backstageImageRenderer: {
    image: { thumbnails: [{ url: "https://img.test/single.jpg", width: 640, height: 480 }] },
  } } });
  assert.deepEqual(mapPost(post).images, [{ url: "https://img.test/single.jpg", width: 640, height: 480 }]);
});

function continuationNode(token, view = false) {
  const command = { continuationCommand: { token, request: "CONTINUATION_REQUEST_TYPE_BROWSE" } };
  return view
    ? { continuationItemViewModel: { continuationCommand: { innertubeCommand: command } } }
    : { continuationItemRenderer: { trigger: "CONTINUATION_TRIGGER_ON_ITEM_SHOWN", continuationEndpoint: command } };
}

function feedWithContinuations(bodyToken, { headerToken, view = false } = {}) {
  const raw = { onResponseReceivedActions: [{ appendContinuationItemsAction: {
    continuationItems: bodyToken === undefined ? [] : [continuationNode(bodyToken, view)],
  } }] };
  if (headerToken !== undefined) {
    const panel = {
      engagementPanel: { engagementPanelSectionListRenderer: { content: continuationNode(headerToken) } },
      engagementPanelPresentationConfigs: { engagementPanelPopupPresentationConfig: { popupType: "ENGAGEMENT_PANEL_POPUP_TYPE_FLOATING" } },
      identifier: { surface: "ENGAGEMENT_PANEL_SURFACE_BROWSE", tag: "engagement-panel-about-channel" },
    };
    raw.header = { pageHeaderRenderer: { content: { pageHeaderViewModel: { description: { descriptionPreviewViewModel: {
      description: { content: "About" }, rendererContext: { commandContext: { onTap: { innertubeCommand: { showEngagementPanelEndpoint: panel } } } },
    } } } } } };
  }
  return new Mixins.Feed({}, Parser.parseResponse(raw), true);
}

test("real Feed reads ContinuationItem and ContinuationItemView body tokens", () => {
  for (const view of [false, true]) {
    const feed = feedWithContinuations("body-token", { view });
    assert.equal(feed.has_continuation, true);
    assert.equal(continuationToken(feed), "body-token");
  }
});

test("real Feed excludes about continuations from its body cursor", () => {
  assert.equal(continuationToken(feedWithContinuations("body-token", { headerToken: "about-token" })), "body-token");
  const headerOnly = feedWithContinuations(undefined, { headerToken: "about-token" });
  assert.equal(headerOnly.has_continuation, false);
  assert.equal(continuationToken(headerOnly), "");
});

test("repeated real Feed body tokens terminate before the page budget is exhausted", async () => {
  const feed = feedWithContinuations("repeated-token");
  let calls = 0;
  const result = await paginate({
    firstPage: feed, getContinuation: async () => { calls++; return feed; },
    mapPage: () => ({ recognized_shape: true, items: [] }), maxPages: 5, maxResults: 10,
    reservedEnvelopeBytes: paginationEnvelopeReserve({ items: [] }),
  });
  assert.equal(result.termination_reason, "cursor_loop");
  assert.equal(result.page_count, 2);
  assert.equal(result.cursor_start, "repeated-token");
  assert.equal(result.cursor_end, "repeated-token");
  assert.equal(calls, 1);
});
