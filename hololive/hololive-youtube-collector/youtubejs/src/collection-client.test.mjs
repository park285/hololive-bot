import assert from "node:assert/strict";
import test from "node:test";
import { Innertube } from "youtubei.js";
import { createCollectionClient } from "./collection-client.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { runWithRequestContext } from "./request-context.mjs";

const scenarios = [
  { tab: "videos", kind: "videos", fetch: fetchContentFeed, rows: "items" },
  { tab: "shorts", kind: "shorts", fetch: fetchContentFeed, rows: "items" },
  { tab: "posts", fetch: fetchCommunityFeed, rows: "posts" },
  { tab: "streams", kind: "live", fetch: fetchChannelFeed, rows: "live_sessions" },
];
const endpoint = tab => ({
  commandMetadata: { webCommandMetadata: { url: `/channel/UC_TEST/${tab}`, apiUrl: "/youtubei/v1/browse" } },
  browseEndpoint: { browseId: "UC_TEST", params: `opaque-${tab}`, query: "preserved-query" },
});
const body = rows => ({ sectionListRenderer: { contents: rows.length ? [{ itemSectionRenderer: { contents: rows } }] : [] } });
const tabNode = (tab, selected = false, rows = []) => ({ tabRenderer: { title: tab, selected, endpoint: endpoint(tab), content: body(rows) } });
const page = tabs => ({ contents: { twoColumnBrowseResultsRenderer: { tabs } } });
const initial = tab => page([tabNode("featured", true), tabNode(tab)]);
const navigate = tab => ({ onResponseReceivedActions: [{ navigateAction: { endpoint: endpoint(tab) } }] });
const rowNode = (tab, id = "row-1", malformed = false) => tab === "posts"
  ? { backstagePostRenderer: {
    postId: id, contentText: malformed ? { runs: [null] } : { simpleText: id },
    authorText: { simpleText: "Channel" }, authorEndpoint: { browseEndpoint: { browseId: "UC_TEST" } },
  } }
  : { videoRenderer: {
    videoId: id, title: malformed ? { runs: [null] } : { simpleText: id },
    ...(tab === "streams" ? { badges: [{ metadataBadgeRenderer: { label: "LIVE" } }] } : {}),
  } };
const continuation = token => ({ continuationItemRenderer: {
  trigger: "CONTINUATION_TRIGGER_ON_ITEM_SHOWN",
  continuationEndpoint: {
    commandMetadata: { webCommandMetadata: { apiUrl: "/youtubei/v1/browse" } },
    continuationCommand: { token, request: "CONTINUATION_REQUEST_TYPE_BROWSE" },
  },
} });
const continued = rows => ({ onResponseReceivedActions: [{ appendContinuationItemsAction: { continuationItems: rows } }] });

async function localClient(respond) {
  const requests = [];
  const native = await Innertube.create({
    retrieve_player: false, generate_session_locally: true, enable_session_cache: false,
    retrieve_innertube_config: false,
    fetch: async (input, init) => {
      const request = input instanceof Request ? input : new Request(input, init);
      requests.push({ url: request.url, method: request.method, body: JSON.parse(await request.text()) });
      const data = typeof respond === "function" ? await respond(requests.length, requests.at(-1)) : respond[requests.length - 1];
      assert.notEqual(data, undefined, "unexpected physical request");
      return new Response(JSON.stringify(data), { headers: { "content-type": "application/json" } });
    },
  });
  return { client: createCollectionClient(native), native, requests };
}
function collect(scenario, client) {
  return scenario.fetch({ channelId: "UC_TEST", kind: scenario.kind, innertube: client, maxPages: 3, maxResults: 100 });
}
async function rejectsDrift(scenario, responses, calls = responses.length) {
  const { client, requests } = await localClient(responses);
  await assert.rejects(collect(scenario, client), error => error.code === "parser_drift");
  assert.equal(requests.length, calls);
}

for (const scenario of scenarios) {
  test(`${scenario.tab} requires every raw inventory slot to prove absence`, async () => {
    for (const slot of [null, {}, false, 0, "", { tabRenderer: {} }]) {
      await rejectsDrift(scenario, [page([tabNode("featured", true), slot])]);
    }
    const { client, requests } = await localClient([page([tabNode("featured", true)])]);
    const result = await collect(scenario, client);
    assert.equal(result.missing_tab, true);
    assert.equal(result.exhausted, true);
    assert.deepEqual(result[scenario.rows], []);
    assert.equal(requests.length, 1);
  });

  test(`${scenario.tab} needs recognized channel routes and consistent ownership for absence`, async () => {
    for (const url of ["garbage", "/watch?v=x", "/feed/history", "/channel/UC_OTHER/featured"]) {
      const other = tabNode("featured", true);
      other.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url = url;
      await rejectsDrift(scenario, [page([other])]);
    }
    const foreign = tabNode("featured", true);
    foreign.tabRenderer.endpoint.browseEndpoint.browseId = "UC_OTHER";
    await rejectsDrift(scenario, [page([foreign])]);
    for (const url of ["/channel/UC_TEST", "/@handle", "/c/name", "/user/name"]) {
      const home = tabNode("featured", true);
      home.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url = url;
      const search = { expandableTabRenderer: tabNode("search").tabRenderer };
      const { client, requests } = await localClient([page([home, search])]);
      const result = await collect(scenario, client);
      assert.equal(result.missing_tab, true);
      assert.equal(requests.length, 1);
    }
  });

  test(`${scenario.tab} rejects malformed raw selection and slots even with a valid target`, async () => {
    for (const selected of ["true", 1, null]) {
      const other = tabNode("featured");
      other.tabRenderer.selected = selected;
      for (const response of [page([other]), page([other, tabNode(scenario.tab, true)])]) {
        await rejectsDrift(scenario, [response]);
      }
    }
    await rejectsDrift(scenario, [page([tabNode("featured", true), tabNode("playlists", true)])]);
    for (const slot of [null, {}]) {
      await rejectsDrift(scenario, [page([tabNode(scenario.tab, true), slot])]);
    }
  });

  test(`${scenario.tab} accepts positive evidence with an unrelated optional URL missing`, async () => {
    for (const alreadySelected of [false, true]) {
      const unrelated = tabNode("featured", !alreadySelected);
      delete unrelated.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url;
      const loadedUnrelated = structuredClone(unrelated);
      loadedUnrelated.tabRenderer.selected = false;
      const rows = [rowNode(scenario.tab)];
      const { client, requests } = await localClient([
        page([unrelated, tabNode(scenario.tab, alreadySelected, alreadySelected ? rows : [])]),
        page([loadedUnrelated, tabNode(scenario.tab, true, rows)]),
      ]);
      const result = await collect(scenario, client);
      assert.equal(result.missing_tab, undefined);
      assert.equal(result[scenario.rows].length, 1);
      assert.equal(requests.length, alreadySelected ? 1 : 2);
    }
  });

  test(`${scenario.tab} matches route pathname without rewriting endpoint parameters`, async () => {
    for (const url of [
      `/channel/UC_TEST/${scenario.tab}?view=0`, `/channel/UC_TEST/${scenario.tab}/`,
      `/@handle/${scenario.tab}/?view=0#anchor`, `https://www.youtube.com/c/name/${scenario.tab}`, `http://youtube.com/user/name/${scenario.tab}`,
    ]) {
      const target = tabNode(scenario.tab);
      target.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url = url;
      const loaded = structuredClone(target);
      loaded.tabRenderer.selected = true;
      const { client, requests } = await localClient([page([tabNode("featured", true), target]), page([loaded])]);
      const result = await collect(scenario, client);
      assert.equal(result.missing_tab, undefined);
      assert.equal(result.exhausted, true);
      assert.equal(requests.length, 2);
      assert.deepEqual(requests.map(request => request.method), ["POST", "POST"]);
      assert.equal(requests[0].body.browseId, "UC_TEST");
      assert.equal(requests[1].body.browseId, "UC_TEST");
      assert.equal(requests[1].body.params, `opaque-${scenario.tab}`);
      assert.equal(requests[1].body.query, "preserved-query");
    }
  });

  test(`${scenario.tab} rejects ambiguous selection and missing or unrecognized selected bodies at both boundaries`, async () => {
    const invalidPages = [
      page([tabNode("featured", true), tabNode(scenario.tab)]),
      page([tabNode("featured"), tabNode(scenario.tab)]),
      page([tabNode("featured", true), tabNode(scenario.tab, true)]),
      page([tabNode(scenario.tab, true), tabNode(scenario.tab)]),
    ];
    for (const content of [undefined, null, {}, { sectionListRenderer: {} },
      { sectionListRenderer: { contents: null } }, { sectionListRenderer: { contents: [null] } },
      { sectionListRenderer: { contents: [{}] } },
      ...[undefined, null, [null], [{}]].map(contents => ({ sectionListRenderer: { contents: [{ itemSectionRenderer: { contents } }] } })),
      ...[undefined, null, [null], [{}]].map(contents => ({ richGridRenderer: { contents: [{ itemSectionRenderer: { contents } }] } })),
    ]) {
      const target = tabNode(scenario.tab, true);
      target.tabRenderer.content = content;
      invalidPages.push(page([target]));
    }
    for (const response of invalidPages) {
      await rejectsDrift(scenario, [initial(scenario.tab), response]);
      if (response.contents.twoColumnBrowseResultsRenderer.tabs.some(tab => tab.tabRenderer.selected && tab.tabRenderer.title === scenario.tab)) {
        await rejectsDrift(scenario, [response]);
      }
    }
    await rejectsDrift(scenario, [initial(scenario.tab), page([tabNode("featured", true)])]);
  });

  test(`${scenario.tab} recognizes empty section, item-section and rich-grid lists`, async () => {
    for (const content of [body([]),
      { sectionListRenderer: { contents: [{ itemSectionRenderer: { contents: [] } }] } },
      { richGridRenderer: { contents: [] } },
    ]) {
      const target = tabNode(scenario.tab, true);
      target.tabRenderer.content = content;
      const { client, requests } = await localClient([page([target])]);
      const result = await collect(scenario, client);
      assert.equal(result.missing_tab, undefined);
      assert.deepEqual(result[scenario.rows], []);
      assert.equal(result.exhausted, true);
      assert.equal(requests.length, 1);
    }
  });

  test(`${scenario.tab} preserves channel identity at the initial and loaded boundaries`, async () => {
    const foreignPage = { ...page([tabNode(scenario.tab, true)]), metadata: { channelMetadataRenderer: { externalId: "UC_OTHER" } } };
    const foreignEndpoint = tabNode(scenario.tab, true);
    foreignEndpoint.tabRenderer.endpoint.browseEndpoint.browseId = "UC_OTHER";
    for (const response of [foreignPage, page([foreignEndpoint])]) {
      await rejectsDrift(scenario, [response]);
      await rejectsDrift(scenario, [initial(scenario.tab), response]);
    }
  });

  test(`${scenario.tab} isolates selected content while retaining original parser diagnostics`, async () => {
    for (const malformed of [false, true]) {
      const unrelated = tabNode("featured", false, [rowNode(scenario.tab, "unrelated", malformed), continuation("unrelated-next")]);
      const { client, requests } = await localClient([
        initial(scenario.tab), page([unrelated, tabNode(scenario.tab, true, [rowNode(scenario.tab)])]),
      ]);
      if (malformed) {
        await assert.rejects(collect(scenario, client), error => error.code === "parser_drift");
      } else {
        const result = await collect(scenario, client);
        assert.deepEqual(result[scenario.rows].map(row => row.video_id ?? row.postId), ["row-1"]);
        assert.equal(result.exhausted, true);
      }
      assert.equal(requests.length, 2);
    }
  });

  test(`${scenario.tab} retains initial navigate chains and does not follow loaded redirects`, async () => {
    const { client, requests } = await localClient([
      navigate("first-hop"), navigate("second-hop"), initial(scenario.tab), page([tabNode(scenario.tab, true)]),
    ]);
    const result = await collect(scenario, client);
    assert.equal(result.exhausted, true);
    assert.equal(requests.length, 4);
    assert.deepEqual(requests.slice(1).map(request => request.body.params), ["opaque-first-hop", "opaque-second-hop", `opaque-${scenario.tab}`]);
    await rejectsDrift(scenario, [initial(scenario.tab), navigate("unexpected")]);
  });

  if (scenario.tab !== "streams") {
    test(`${scenario.tab} preserves real continuation and an empty terminal page`, async () => {
      const { client, requests } = await localClient([
        initial(scenario.tab),
        page([tabNode(scenario.tab, true, [rowNode(scenario.tab), continuation("next-page")])]),
        continued([]),
      ]);
      const result = await collect(scenario, client);
      assert.equal(result[scenario.rows].length, 1);
      assert.equal(result.page_count, 2);
      assert.equal(result.exhausted, true);
      assert.equal(requests.length, 3);
      assert.equal(requests[2].body.continuation, "next-page");
    });
  }
}

test("one shared client isolates concurrent raw evidence and parser failures", async () => {
  const entered = [Promise.withResolvers(), Promise.withResolvers()];
  const released = [Promise.withResolvers(), Promise.withResolvers()];
  const { client, requests } = await localClient(async call => {
    entered[call - 1].resolve();
    await released[call - 1].promise;
    return page([tabNode("videos", true, [rowNode("videos", `row-${call}`, call === 1)])]);
  });
  const pending = [collect(scenarios[0], client), collect(scenarios[0], client)]
    .map(result => result.then(value => ({ value }), error => ({ error })));
  await Promise.all(entered.map(gate => gate.promise));
  released[0].resolve();
  assert.equal((await pending[0]).error.code, "parser_drift");
  released[1].resolve();
  assert.equal((await pending[1]).value.items[0].video_id, "row-2");
  assert.equal(requests.length, 2);
});

test("videos tab lockup times never become item times and only the structured upcoming flag survives", async () => {
  const upcoming = rowNode("videos", "upcoming-row");
  upcoming.videoRenderer.upcomingEventData = { startTime: "4102444800" };
  const premiered = rowNode("videos", "premiered-row");
  premiered.videoRenderer.publishedTimeText = { simpleText: "Premiered Sep 20, 2026" };
  const relative = rowNode("videos", "relative-row");
  relative.videoRenderer.publishedTimeText = { simpleText: "3 hours ago" };
  const { client, requests } = await localClient([page([tabNode("videos", true, [upcoming, premiered, relative])])]);
  const result = await collect(scenarios[0], client);
  assert.deepEqual(result.items, [
    { video_id: "upcoming-row", channel_id: "UC_TEST", title: "upcoming-row", is_upcoming: true },
    { video_id: "premiered-row", channel_id: "UC_TEST", title: "premiered-row" },
    { video_id: "relative-row", channel_id: "UC_TEST", title: "relative-row" },
  ]);
  assert.equal(requests.length, 1);
  assert.ok(requests.every(request => !request.url.includes("/player")));
});

// 실제 streams/videos 탭의 lockupViewModel 형태입니다. 배지는 thumbnailBottomOverlayViewModel에 실립니다.
// null은 해당 필드를 원문에서 빼라는 뜻입니다.
const lockupRow = (id, badges, { contentType = "LOCKUP_CONTENT_TYPE_VIDEO", title = { content: id } } = {}) => ({ lockupViewModel: {
  contentId: id, ...(contentType === null ? {} : { contentType }),
  contentImage: { thumbnailViewModel: { image: { sources: [] }, overlays: [{ thumbnailBottomOverlayViewModel: { badges } }] } },
  metadata: { lockupMetadataViewModel: { ...(title === null ? {} : { title }) } },
} });
const liveBadge = { thumbnailBadgeViewModel: { text: "LIVE", badgeStyle: "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE" } };
const upcomingBadge = { thumbnailBadgeViewModel: { text: "Upcoming", badgeStyle: "THUMBNAIL_OVERLAY_BADGE_STYLE_DEFAULT" } };
const durationBadge = { thumbnailBadgeViewModel: { text: "1:02:03", badgeStyle: "THUMBNAIL_OVERLAY_BADGE_STYLE_DEFAULT" } };
const richItem = row => ({ richItemRenderer: { content: row } });
const grid = (tab, rows) => ({ tabRenderer: { title: tab, selected: true, endpoint: endpoint(tab), content: { richGridRenderer: { contents: rows } } } });

test("streams lockups take their status from structured badges only", async () => {
  const rows = [lockupRow("live-row", [liveBadge]), lockupRow("upcoming-row", [upcomingBadge]), lockupRow("ended-row", [durationBadge])];
  // lockup에는 기계가독 예정 시각이 없으므로 UPCOMING 행만 raw /player로 보강합니다.
  const player = { videoDetails: { videoId: "upcoming-row", isUpcoming: true, isLiveContent: true }, playabilityStatus: { status: "OK" },
    microformat: { playerMicroformatRenderer: { liveBroadcastDetails: { isLiveNow: false, startTimestamp: "2100-01-01T00:00:00Z" } } } };
  const { client, requests } = await localClient([page([grid("streams", rows.map(richItem))]), player]);
  const result = await collect(scenarios[3], client);
  assert.deepEqual(result.live_sessions.map(session => [session.video_id, session.status, session.scheduled_at]),
    [["live-row", "LIVE", undefined], ["upcoming-row", "UPCOMING", "2100-01-01T00:00:00.000Z"], ["ended-row", "ENDED", undefined]]);
  assert.equal(requests.length, 2);
  assert.equal(requests[1].body.videoId, "upcoming-row");
  // 배지가 없는 lockup은 종료로 추정하지 않습니다.
  await rejectsDrift(scenarios[3], [page([grid("streams", [richItem(lockupRow("unmarked-row", []))])])]);
});

test("streams video renderers become LIVE or UPCOMING only from structured markers", async () => {
  const upcoming = { videoRenderer: { videoId: "upcoming-row", title: { simpleText: "upcoming-row" }, upcomingEventData: { startTime: "4102444800" } } };
  const pastUpcoming = { videoRenderer: { videoId: "late-row", title: { simpleText: "late-row" }, upcomingEventData: { startTime: "946684800" } } };
  const live = { videoRenderer: { videoId: "live-row", title: { simpleText: "live-row" },
    badges: [{ metadataBadgeRenderer: { style: "BADGE_STYLE_TYPE_LIVE_NOW" } }] } };
  const ended = { videoRenderer: { videoId: "ended-row", title: { simpleText: "ended-row" },
    thumbnailOverlays: [{ thumbnailOverlayTimeStatusRenderer: { style: "DEFAULT", text: { simpleText: "12:34" } } }] } };
  const { client } = await localClient([page([tabNode("streams", true, [upcoming, pastUpcoming, live, ended])])]);
  const result = await collect(scenarios[3], client);
  assert.deepEqual(result.live_sessions.map(session => [session.video_id, session.status]),
    [["upcoming-row", "UPCOMING"], ["late-row", "UPCOMING"], ["live-row", "LIVE"], ["ended-row", "ENDED"]]);
  await rejectsDrift(scenarios[3], [page([tabNode("streams", true, [{ videoRenderer: { videoId: "unmarked-row" } }])])]);
});

for (const scenario of scenarios) {
  const row = scenario.tab === "posts" ? rowNode("posts") : richItem(lockupRow("row-1", [durationBadge]));
  const unknown = { unknownRowRenderer: { id: "row-2" } };
  test(`${scenario.tab} rejects body rows the SDK list leaves out or adds`, async () => {
    // SDK가 분류하지 못한 renderer, contentType 없는 lockup, null 행은 진단 없이 목록에서 빠집니다.
    for (const dropped of [unknown, richItem(unknown), richItem(lockupRow("row-2", [durationBadge], { contentType: null })), richItem(null)]) {
      await rejectsDrift(scenario, [page([grid(scenario.tab, [row, dropped])])]);
    }
    // 중첩 shelf의 항목은 본문 행 하나에 여러 항목을 더합니다.
    const shelf = { richSectionRenderer: { content: { richShelfRenderer: { contents: [richItem(lockupRow("shelf-1", [durationBadge])), richItem(lockupRow("shelf-2", [durationBadge]))] } } } };
    await rejectsDrift(scenario, [page([grid(scenario.tab, [row, shelf])])]);
    // 같은 검사가 continuation 페이지에도 적용됩니다. streams 수집은 첫 페이지만 읽습니다.
    if (scenario.tab === "streams") return;
    await rejectsDrift(scenario, [page([grid(scenario.tab, [row, continuation("next")])]), continued([row, unknown])]);
    const { client, requests } = await localClient([page([grid(scenario.tab, [row, continuation("next")])]), continued([row])]);
    const result = await collect(scenario, client);
    assert.equal(result[scenario.rows].length, 2);
    assert.equal(requests.length, 2);
    assert.equal(requests[1].body.continuation, "next");
  });
}

test("streams rows also take LIVE and UPCOMING from time-status overlays and badge text", async () => {
  const timeStatus = (style, text) => ({ thumbnailOverlayTimeStatusRenderer: { style, text: { simpleText: text } } });
  const liveOverlay = { videoRenderer: { videoId: "live-overlay", title: { simpleText: "live-overlay" }, thumbnailOverlays: [timeStatus("LIVE", "LIVE")] } };
  const upcomingOverlay = { videoRenderer: { videoId: "upcoming-overlay", title: { simpleText: "upcoming-overlay" },
    upcomingEventData: { startTime: "4102444800" }, thumbnailOverlays: [timeStatus("UPCOMING", "UPCOMING")] } };
  const textOnlyLive = richItem(lockupRow("live-text", [{ thumbnailBadgeViewModel: { text: "LIVE", badgeStyle: "THUMBNAIL_OVERLAY_BADGE_STYLE_DEFAULT" } }]));
  const { client } = await localClient([page([grid("streams", [liveOverlay, upcomingOverlay, textOnlyLive].map(row => row.richItemRenderer ? row : richItem(row)))])]);
  const result = await collect(scenarios[3], client);
  assert.deepEqual(result.live_sessions.map(session => [session.video_id, session.status]),
    [["live-overlay", "LIVE"], ["upcoming-overlay", "UPCOMING"], ["live-text", "LIVE"]]);
});

test("posts count a shared post as one row and drop its embedded original", async () => {
  const post = id => ({ backstagePostRenderer: { postId: id, contentText: { simpleText: id },
    authorText: { simpleText: "Channel" }, authorEndpoint: { browseEndpoint: { browseId: "UC_TEST" } } } });
  const thread = inner => ({ backstagePostThreadRenderer: { post: inner } });
  const shared = { sharedPostRenderer: { postId: "shared-1", content: { simpleText: "shared" }, displayName: { simpleText: "Channel" }, originalPost: post("original-1") } };
  const { client } = await localClient([page([grid("posts", [thread(post("post-1")), thread(shared)])])]);
  const result = await collect(scenarios[2], client);
  assert.deepEqual(result.posts.map(item => item.postId), ["post-1", "shared-1"]);
});

test("continuation pages chain through endpoint-style responses with raw row accounting", async () => {
  const row = id => richItem(lockupRow(id, [durationBadge]));
  const continuedEndpoints = rows => ({ onResponseReceivedEndpoints: [{ appendContinuationItemsAction: { continuationItems: rows } }] });
  const { client, requests } = await localClient([
    page([grid("videos", [row("v1"), continuation("t1")])]),
    continuedEndpoints([row("v2"), continuation("t2")]),
    continued([row("v3")]),
  ]);
  const result = await collect(scenarios[0], client);
  assert.deepEqual(result.items.map(item => item.video_id), ["v1", "v2", "v3"]);
  assert.deepEqual(requests.slice(1).map(request => request.body.continuation), ["t1", "t2"]);
  await rejectsDrift(scenarios[0], [page([grid("videos", [row("v1"), continuation("t1")])]), continuedEndpoints([row("v2"), { unknownRowRenderer: {} }])]);
});

test("videos lockups without a title node are rejected instead of becoming empty titles", async () => {
  await rejectsDrift(scenarios[0], [page([grid("videos", [richItem(lockupRow("untitled", [durationBadge], { title: null }))])])]);
});

test("request cancellation stops before the first request, a tab load, or another navigation", async () => {
  for (const stage of ["before", "initial", "navigate", "loaded"]) {
    const controller = new AbortController();
    const { client, requests } = await localClient(call => {
      if (stage !== "loaded" || call === 2) controller.abort();
      return stage === "navigate" ? navigate("ignored") : call === 1 ? initial("videos") : page([tabNode("videos", true)]);
    });
    if (stage === "before") controller.abort();
    await assert.rejects(runWithRequestContext({ requestId: stage, signal: controller.signal }, () => collect(scenarios[0], client)),
      error => error.name === "AbortError");
    assert.equal(requests.length, stage === "before" ? 0 : stage === "loaded" ? 2 : 1);
  }
});

test("metadata keeps its original about tab and player methods retain their native receiver", async () => {
  const about = { channelAboutFullMetadataRenderer: {
    channelId: "UC_TEST", title: { simpleText: "Channel" }, description: { simpleText: "About the channel" },
  } };
  const { client, native, requests } = await localClient([
    page([tabNode("about", true, [about])]),
    { videoDetails: { videoId: "video-1", title: "Video" }, playabilityStatus: { status: "OK" } },
  ]);
  const result = await fetchChannelFeed({ channelId: "UC_TEST", kind: "metadata", innertube: client });
  assert.equal(result.profile.description, "About the channel");
  assert.deepEqual(result.live_sessions, []);
  assert.equal(requests.length, 1);
  assert.equal(client.actions, native.actions);
  assert.equal(client.session, native.session);
  const info = await client.getBasicInfo("video-1");
  assert.equal(info.basic_info.id, "video-1");
  assert.equal(requests.length, 2);
  assert.equal(requests[1].body.videoId, "video-1");
});
