// @ts-check

import { currentRequestSignal } from "./request-context.mjs";
import { runUpstream } from "./upstream-errors.mjs";
import { inspectChannelTab } from "./collection-client.mjs";

/** @typedef {import("youtubei.js").YT.Channel} Channel */

/** 원시 목록이 증명하는 부재만 missing으로 반환합니다.
 * @param {Channel} channel
 * @param {string} tab
 * @param {() => Promise<Channel>} load
 * @returns {Promise<{ missing: true } | { missing: false, feed: Channel }>}
 */
export async function fetchChannelTab(channel, tab, load) {
  if (currentRequestSignal()?.aborted) throw new DOMException("aborted", "AbortError");
  if (inspectChannelTab(channel, tab) === null) return { missing: true };
  const feed = await runUpstream(load);
  if (currentRequestSignal()?.aborted) throw new DOMException("aborted", "AbortError");
  inspectChannelTab(feed, tab, true);
  return { missing: false, feed };
}
