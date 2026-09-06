import { readFile, writeFile } from "node:fs/promises";

import { createInnertube } from "../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-community.mjs";
import { mapContentItems } from "../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-content.mjs";
import { paginate, paginationEnvelopeReserve } from "../../../hololive/hololive-youtube-collector/youtubejs/src/pagination.mjs";

const outputURL = new URL("./public-titles.json", import.meta.url);
const rules = JSON.parse(await readFile(new URL("../../../hololive/hololive-shared/pkg/domain/mekparkhost/rules.json", import.meta.url), "utf8"));
const client = await createInnertube({
  fetchImpl: (input, init) => fetch(input, { ...init, signal: AbortSignal.timeout(20_000) }),
});
const observedAt = new Date().toISOString();
const videos = new Map();
const tabs = [];
const tabLoaders = { videos: "getVideos", streams: "getLiveStreams", shorts: "getShorts" };

for (const [unit, definition] of Object.entries(rules.units)) {
  const channelID = definition.channel_id;
  const channel = await client.getChannel(channelID);
  for (const [tab, loader] of Object.entries(tabLoaders)) {
    const sourceURL = `https://www.youtube.com/channel/${channelID}/${tab}`;
    try {
      const feed = await channel[loader]();
      const result = await paginate({
        firstPage: feed,
        getContinuation: (page) => page.getContinuation(),
        mapPage: (page) => ({ recognized_shape: true, items: mapContentItems(page, channelID) }),
        maxPages: 20,
        maxResults: 1000,
        maxSuccessResponseBytes: 2 * 1024 * 1024,
        reservedEnvelopeBytes: paginationEnvelopeReserve({ items: [] }),
        buildResult: (items, pagination) => ({ items, ...pagination }),
      });
      for (const item of result.items) {
        if (!/^[A-Za-z0-9_-]{11}$/.test(item.video_id) || item.channel_id !== channelID || !item.title) {
          throw new Error("public title identity or channel validation failed");
        }
        const previous = videos.get(item.video_id);
        if (previous) {
          if (previous.channel_id !== channelID) throw new Error("video belongs to multiple channels");
          previous.sources.push(sourceURL);
          previous.tabs.push(tab);
        } else {
          videos.set(item.video_id, {
            video_id: item.video_id,
            channel_id: channelID,
            unit,
            title: item.title,
            url: `https://www.youtube.com/watch?v=${item.video_id}`,
            tabs: [tab],
            sources: [sourceURL],
          });
        }
      }
      tabs.push({ unit, tab, source_url: sourceURL, count: result.items.length, pages: result.page_count,
        exhausted: result.exhausted, termination_reason: result.termination_reason });
      console.log(JSON.stringify(tabs.at(-1)));
    } catch (error) {
      tabs.push({ unit, tab, source_url: sourceURL, error: String(error.message).slice(0, 180) });
      console.log(JSON.stringify(tabs.at(-1)));
    }
  }
}

const cases = [...videos.values()].sort((a, b) => a.video_id.localeCompare(b.video_id, "en"));
let metadataFailures = 0;
for (let index = 0; index < cases.length; index += 3) {
  const batch = cases.slice(index, index + 3);
  const results = await Promise.allSettled(batch.map(async (item) => {
    const info = await client.getBasicInfo(item.video_id);
    const basic = info.basic_info;
    if (basic.channel_id !== item.channel_id) throw new Error("player channel does not match public listing");
    const microformat = info.page?.[0]?.microformat;
    return {
      ...(basic.title && basic.title !== item.title ? { listing_title: item.title, title: basic.title } : {}),
      metadata_status: "available",
      start_timestamp: basic.start_timestamp?.toISOString() ?? null,
      publish_date: microformat?.publish_date ?? null,
      upload_date: microformat?.upload_date ?? null,
      is_live_content: basic.is_live_content ?? null,
      is_upcoming: basic.is_upcoming ?? null,
    };
  }));
  for (const [offset, result] of results.entries()) {
    if (result.status === "fulfilled") {
      Object.assign(batch[offset], result.value);
    } else {
      metadataFailures++;
      Object.assign(batch[offset], { metadata_status: "unavailable", metadata_error: String(result.reason.message).slice(0, 180) });
    }
  }
  if ((index + 3) % 30 === 0 || index + 3 >= cases.length) {
    console.log(JSON.stringify({ metadata_checked: Math.min(index + 3, cases.length), total: cases.length, metadata_failures: metadataFailures }));
  }
}

const result = {
  schema_version: 1,
  observed_at: observedAt,
  source: "Public YouTube channel tabs and player metadata, unauthenticated YouTube.js 18.0.0",
  limits: { max_pages_per_tab: 20, max_items_per_tab: 1000, request_timeout_ms: 20_000, metadata_concurrency: 3 },
  complete: tabs.every((tab) => tab.exhausted === true),
  tabs,
  metadata_failures: metadataFailures,
  cases,
};
await writeFile(outputURL, JSON.stringify(result, null, 2) + "\n");
console.log(JSON.stringify({ saved: outputURL.pathname, unique_videos: cases.length, complete: result.complete, metadata_failures: metadataFailures }));
if (!result.complete) process.exitCode = 1;
