import assert from "node:assert/strict";
import test from "node:test";
import { Innertube, Parser } from "youtubei.js";
import { createCollectionClient } from "./collection-client.mjs";

import { fetchChannelFeed } from "./fetch-channel.mjs";
import { fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { handleChannelRequest, handleCommunityRequest, handleContentRequest } from "./rpc-boundary.mjs";
import { rpcErrorResultFor } from "./rpc-validation.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";

const scenarios = [
  { tab: "videos", kind: "videos", rows: "items", fetch: fetchContentFeed, handle: handleContentRequest },
  { tab: "shorts", kind: "shorts", rows: "items", fetch: fetchContentFeed, handle: handleContentRequest },
  { tab: "posts", rows: "posts", fetch: fetchCommunityFeed, handle: handleCommunityRequest },
  { tab: "streams", kind: "live", rows: "live_sessions", fetch: fetchChannelFeed, handle: handleChannelRequest },
];

function channelPage(tabs) {
  return { contents: { twoColumnBrowseResultsRenderer: { tabs } } };
}

function tabNode(tab, rows = [], { selected = true, apiUrl = "/youtubei/v1/browse" } = {}) {
  return { tabRenderer: {
    title: tab,
    selected,
    endpoint: {
      commandMetadata: { webCommandMetadata: { url: `/channel/UC_TEST/${tab}`, apiUrl } },
      browseEndpoint: { browseId: "UC_TEST" },
    },
    content: { sectionListRenderer: { contents: [{ itemSectionRenderer: { contents: rows } }] } },
  } };
}

function rowNode(tab, malformed = false) {
  const text = malformed ? { runs: [null] } : { simpleText: "valid row" };
  if (tab === "posts") {
    return { backstagePostRenderer: {
      postId: "post-1", contentText: text,
      authorText: { simpleText: "channel" }, authorEndpoint: { browseEndpoint: { browseId: "UC_TEST" } },
    } };
  }
  return { videoRenderer: {
    videoId: "video-1", title: text,
    ...(tab === "streams" ? { badges: [{ metadataBadgeRenderer: { label: "LIVE" } }] } : {}),
  } };
}

function continuationNode() {
  return { continuationItemRenderer: {
    trigger: "CONTINUATION_TRIGGER_ON_ITEM_SHOWN",
    continuationEndpoint: {
      commandMetadata: { webCommandMetadata: { apiUrl: "/youtubei/v1/browse" } },
      continuationCommand: { token: "next-page", request: "CONTINUATION_REQUEST_TYPE_BROWSE" },
    },
  } };
}

async function rawClient(respond) {
  let calls = 0;
  const innertube = await Innertube.create({
    retrieve_player: false, generate_session_locally: true, enable_session_cache: false,
    retrieve_innertube_config: false,
    fetch: async () => new Response(JSON.stringify(await respond(++calls))),
  });
  return { innertube: createCollectionClient(innertube), calls: () => calls };
}

function collect(scenario, innertube) {
  return scenario.handle(JSON.stringify({
    protocol_version: 1, channel_id: "UC_TEST", max_pages: 3, max_success_response_bytes: 1048576,
    ...(scenario.kind === undefined ? {} : { kind: scenario.kind }),
  }), options => scenario.fetch({ ...options, innertube }));
}

function assertParserFailure(result, message = "upstream parser rejected response data") {
  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
  assert.equal(result.body.error.class, "DATA_CONTRACT");
  assert.equal(result.body.error.message, message);
  assert.equal(result.body.missing_tab, undefined);
  assert.equal(result.body.exhausted, undefined);
}

for (const scenario of scenarios) {
  for (const stage of ["initial", "loaded"]) {
    test(`${scenario.tab} rejects unrecognized ${stage} tab lists without parser diagnostics`, async () => {
      const missingURL = tabNode(scenario.tab);
      delete missingURL.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url;
      const blankURL = tabNode(scenario.tab);
      blankURL.tabRenderer.endpoint.commandMetadata.webCommandMetadata.url = " ";
      for (const tabs of [
        undefined, null, [], [{ tabRenderer: {} }], [{ tabRenderer: { title: "Unknown" } }],
        [missingURL], [blankURL], [tabNode("featured", [], { selected: false }), missingURL],
      ]) {
        const client = await rawClient(call => stage === "loaded" && call === 1
          ? channelPage([tabNode(scenario.tab, [], { selected: false })])
          : channelPage(tabs));
        assertParserFailure(await collect(scenario, client.innertube), "channel tab list is not recognized");
        assert.equal(client.calls(), stage === "initial" ? 1 : 2);
      }
    });
  }

  test(`${scenario.tab} accepts an empty recognized tab returned by its loader`, async () => {
    const client = await rawClient(call => channelPage([tabNode(scenario.tab, [], { selected: call === 2 })]));
    const result = await collect(scenario, client.innertube);
    assert.equal(result.status, 200);
    assert.equal(result.body.missing_tab, undefined);
    assert.equal(result.body[scenario.rows].length, 0);
    assert.equal(result.body.exhausted, true);
    assert.equal(client.calls(), 2);
  });

  test(`${scenario.tab} rejects a tab discarded by the real parser`, async () => {
    const client = await rawClient(() => channelPage([tabNode(scenario.tab, [], { apiUrl: 17 })]));
    assertParserFailure(await collect(scenario, client.innertube));
    assert.equal(client.calls(), 1);
  });

  test(`${scenario.tab} rejects parsing failure while loading its tab`, async () => {
    const client = await rawClient(call => channelPage([
      tabNode(scenario.tab, [], { selected: call !== 1, ...(call === 1 ? {} : { apiUrl: 17 }) }),
    ]));
    assertParserFailure(await collect(scenario, client.innertube));
    assert.equal(client.calls(), 2);
  });

  test(`${scenario.tab} rejects a failed nested row instead of an empty success`, async () => {
    const client = await rawClient(() => channelPage([tabNode(scenario.tab, [rowNode(scenario.tab, true)])]));
    assertParserFailure(await collect(scenario, client.innertube));
    assert.equal(client.calls(), 1);
  });

  test(`${scenario.tab} preserves real missing, empty and populated tab results after failure`, async () => {
    const client = await rawClient(call => channelPage([
      call === 1 ? tabNode(scenario.tab, [], { apiUrl: 17 })
        : call === 2 ? tabNode("featured")
          : tabNode(scenario.tab, call === 3 ? [] : [rowNode(scenario.tab)]),
    ]));
    assertParserFailure(await collect(scenario, client.innertube));
    const missing = await collect(scenario, client.innertube);
    assert.equal(missing.status, 200);
    assert.equal(missing.body.missing_tab, true);
    const empty = await collect(scenario, client.innertube);
    assert.equal(empty.status, 200);
    assert.equal(empty.body.missing_tab, undefined);
    assert.equal(empty.body[scenario.rows].length, 0);
    const populated = await collect(scenario, client.innertube);
    assert.equal(populated.status, 200);
    assert.equal(populated.body.missing_tab, undefined);
    assert.equal(populated.body[scenario.rows].length, 1);
    assert.equal(client.calls(), 4);
  });
}

for (const scenario of scenarios.filter(scenario => scenario.tab !== "streams")) {
  test(`${scenario.tab} continuation parsing failure rejects the validated prefix`, async () => {
    const client = await rawClient(call => call === 1
      ? channelPage([tabNode(scenario.tab, [rowNode(scenario.tab), continuationNode()])])
      : { onResponseReceivedActions: [{ appendContinuationItemsAction: {
        continuationItems: [rowNode(scenario.tab, true)],
      } }] });
    const result = await collect(scenario, client.innertube);
    assertParserFailure(result);
    assert.equal(result.body[scenario.rows], undefined);
    assert.equal(client.calls(), 2);
  });
}

test("real parser typecheck failures cannot discard tab contents silently", async () => {
  const tab = tabNode("videos");
  tab.tabRenderer.content = rowNode("videos");
  const client = await rawClient(() => channelPage([tab]));
  assertParserFailure(await collect(scenarios[0], client.innertube));
});

test("overlapping parser calls keep failure ownership with their request", async () => {
  const entered = [Promise.withResolvers(), Promise.withResolvers()];
  const released = [Promise.withResolvers(), Promise.withResolvers()];
  const clients = await Promise.all([true, false].map((malformed, index) => rawClient(async () => {
    entered[index].resolve();
    await released[index].promise;
    return channelPage([tabNode("videos", [rowNode("videos", malformed)])]);
  })));
  const results = clients.map(client => collect(scenarios[0], client.innertube));
  await Promise.all(entered.map(gate => gate.promise));
  released[0].resolve();
  assertParserFailure(await results[0]);
  released[1].resolve();
  const valid = await results[1];
  assert.equal(valid.status, 200);
  assert.equal(valid.body.items.length, 1);
  assert.deepEqual(clients.map(client => client.calls()), [1, 1]);
});

test("successful unknown-node generation and schema updates remain usable", async () => {
  const client = await rawClient(call => channelPage([tabNode("videos", [
    rowNode("videos"),
    { collectorParserDiagnosticRenderer: { label: "diagnostic", ...(call === 1 ? {} : { additionalField: true }) } },
  ])]));
  for (let call = 1; call <= 2; call++) {
    const result = await collect(scenarios[0], client.innertube);
    assert.equal(result.status, 200);
    assert.equal(result.body.items.length, 1);
  }
  assert.equal(client.calls(), 2);
});

test("parser cleanup finishes before its scoped failure is raised", async () => {
  let parsed;
  await assert.rejects(runUpstream(async () => {
    parsed = Parser.parseResponse(channelPage([tabNode("featured"), tabNode("videos", [], { apiUrl: 17 })]));
  }), error => error.code === "parser_drift" && error.cause === undefined);
  assert.ok(parsed.contents_memo);
  const entries = [...parsed.contents_memo];
  Parser.parseItem({ sectionListRenderer: { contents: [] } });
  assert.deepEqual([...parsed.contents_memo], entries);
});

test("unowned parsing does not leave failure state on later calls", () => {
  const malformed = channelPage([tabNode("videos", [], { apiUrl: 17 })]);
  assert.doesNotThrow(() => Parser.parseResponse(malformed));
  assert.doesNotThrow(() => readUpstream(() => Parser.parseResponse(channelPage([tabNode("videos")]))));
  assert.throws(() => readUpstream(() => Parser.parseResponse(malformed)), error => error.code === "parser_drift");
});

test("a later explicit helper defect takes precedence over a swallowed parsing error", async () => {
  for (const code of ["helper_internal_invariant", "helper_protocol_mismatch", "collection_canceled"]) {
    const defect = Object.assign(new Error("explicit helper defect"), { code });
    const tab = tabNode("videos");
    Object.defineProperty(tab.tabRenderer.endpoint.commandMetadata.webCommandMetadata, "apiUrl", {
      get() { throw defect; },
    });
    const raw = channelPage([tabNode("shorts", [], { apiUrl: 17 }), tab]);
    await assert.rejects(runUpstream(async () => Parser.parseResponse(raw)), error => error === defect);
    assert.throws(() => readUpstream(() => Parser.parseResponse(raw)), error => error === defect);
  }
});

test("parent cancellation and independent AbortError keep their classifications", async () => {
  const raw = channelPage([tabNode("videos", [], { apiUrl: 17 })]);
  const controller = new AbortController();
  const canceled = await runWithRequestContext({ requestId: "parser-canceled", signal: controller.signal }, async () => {
    try {
      await runUpstream(async () => {
        Parser.parseResponse(raw);
        controller.abort();
      });
    } catch (error) {
      return rpcErrorResultFor(error);
    }
  });
  assert.equal(canceled.status, 408);
  assert.equal(canceled.body.error.code, "collection_canceled");

  const abort = new DOMException("unexpected independent abort", "AbortError");
  const tab = tabNode("videos");
  Object.defineProperty(tab.tabRenderer.endpoint.commandMetadata.webCommandMetadata, "apiUrl", {
    get() { throw abort; },
  });
  await assert.rejects(runUpstream(async () => Parser.parseResponse(channelPage([tab]))), error => {
    assert.equal(error, abort);
    assert.equal(rpcErrorResultFor(error).body.error.code, "helper_internal_invariant");
    return true;
  });
});
