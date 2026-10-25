// @ts-check

import { Mixins, Parser, YT, YTNodes } from "youtubei.js";
import { currentRequestSignal } from "./request-context.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";

/** @typedef {import("youtubei.js").Innertube} Innertube */
/** @typedef {Parameters<typeof Parser.parseResponse>[0]} RawPage */
/** rowCount는 검증한 본문의 원문 행 수입니다. 탭 전체 페이지에는 없습니다.
 * @typedef {{ raw: RawPage, channelId: string, rowCount?: number }} ChannelEvidence */
/** @type {WeakMap<object, ChannelEvidence>} */
const evidence = new WeakMap();

/** 원시 응답을 보존하는 수집 전용 클라이언트입니다. SDK 메서드의 this 바인딩을 유지합니다.
 * @param {Innertube} client
 */
export function createCollectionClient(client) {
  return {
    actions: client.actions,
    session: client.session,
    getBasicInfo: client.getBasicInfo.bind(client),
    async getChannel(channelId) {
      return runUpstream(async () => {
        throwIfAborted();
        const endpoint = new YTNodes.NavigationEndpoint({ browseEndpoint: { browseId: channelId } });
        let response = await endpoint.call(client.actions);
        let parsed = readUpstream(() => Parser.parseResponse(response.data));
        // Actions.execute(parse:true)의 초기 browse 이동을 같은 endpoint와 요청 수로 보존합니다.
        let navigate = parsed.on_response_received_actions?.[0];
        while (navigate?.type === "navigateAction" && "endpoint" in navigate) {
          throwIfAborted();
          response = await navigate.endpoint.call(client.actions);
          parsed = readUpstream(() => Parser.parseResponse(response.data));
          navigate = parsed.on_response_received_actions?.[0];
        }
        throwIfAborted();
        return new CollectionChannel(client.actions, parsed, response.data, channelId);
      });
    },
  };
}

/** 파서가 누락시킨 원시 슬롯도 부재 판단에 포함합니다.
 * @param {import("youtubei.js").YT.Channel} channel
 * @param {string} tab
 * @param {boolean} [loaded]
 */
export function inspectChannelTab(channel, tab, loaded = false) {
  const source = evidence.get(channel);
  if (!source) throw drift("channel tab evidence is unavailable");
  return inspectRawTab(source.raw, source.channelId, tab, loaded);
}

class CollectionChannel extends YT.Channel {
  /** @param {Innertube["actions"]} actions
   * @param {ReturnType<typeof Parser.parseResponse>} parsed
   * @param {RawPage} raw
   * @param {string} channelId
   * @param {number} [rowCount]
   */
  constructor(actions, parsed, raw, channelId, rowCount) {
    super(actions, parsed, true);
    evidence.set(this, { raw, channelId, rowCount });
  }

  get videos() { return accountedRows(this, super.videos); }
  get posts() { return accountedRows(this, topLevelPosts(this, super.posts)); }
  async getContinuation() { return loadContinuation(this); }

  async getVideos() { return this.loadCollectionTab("videos"); }
  async getShorts() { return this.loadCollectionTab("shorts"); }
  async getCommunity() { return this.loadCollectionTab("posts"); }
  async getLiveStreams() { return this.loadCollectionTab("streams"); }

  /** @param {string} tab */
  async loadCollectionTab(tab) {
    return runUpstream(async () => {
      throwIfAborted();
      const source = evidence.get(this);
      if (!source) throw drift("channel tab evidence is unavailable");
      const target = inspectRawTab(source.raw, source.channelId, tab);
      if (target === null) throw drift("requested channel tab is unavailable");
      let raw = source.raw;
      if (target.selected !== true) {
        const endpoint = new YTNodes.NavigationEndpoint(target.endpoint);
        const response = await endpoint.call(this.actions);
        raw = response.data;
        // 투영에서 제외되는 원본 노드의 파서 진단도 먼저 보존합니다.
        readUpstream(() => Parser.parseResponse(raw));
      }
      throwIfAborted();
      const selected = inspectRawTab(raw, source.channelId, tab, true);
      if (selected === null) throw drift("requested channel tab is unavailable");
      // SDK memo는 다른 탭·header·sidebar의 영상까지 합칩니다. 검증한 본문만 수집합니다.
      const page = {
        ...(raw.alerts === undefined ? {} : { alerts: raw.alerts }),
        contents: { twoColumnBrowseResultsRenderer: { tabs: [{ tabRenderer: selected }] } },
      };
      const parsed = readUpstream(() => Parser.parseResponse(page));
      return new CollectionChannel(this.actions, parsed, raw, source.channelId, bodyRowCount(selected));
    });
  }
}

/** continuation 페이지도 원문 행 수를 보존합니다. SDK의 ChannelListContinuation과 같은 Feed입니다. */
class CollectionContinuation extends Mixins.Feed {
  /** @param {Innertube["actions"]} actions
   * @param {ReturnType<typeof Parser.parseResponse>} parsed
   * @param {RawPage} raw
   * @param {string} channelId
   * @param {number} rowCount
   */
  constructor(actions, parsed, raw, channelId, rowCount) {
    super(actions, parsed, true);
    evidence.set(this, { raw, channelId, rowCount });
  }

  get videos() { return accountedRows(this, super.videos); }
  get posts() { return accountedRows(this, topLevelPosts(this, super.posts)); }
  async getContinuation() { return loadContinuation(this); }
}

/** SDK 목록은 영상·게시물로 분류하지 못한 본문 행(미지 renderer, contentType 없는 lockup, null)을 진단 없이
 * 빼고, 중첩 shelf의 항목은 더합니다. 원문 행 수와 다르면 조용히 누락·추가하지 않고 drift로 거부합니다.
 * @template {unknown[]} T
 * @param {object} feed @param {T} rows */
function accountedRows(feed, rows) {
  const expected = evidence.get(feed)?.rowCount;
  if (expected !== undefined && rows.length !== expected) {
    throw drift("channel tab rows are not all recognized");
  }
  return rows;
}

/** SharedPost는 original_post를 다시 파싱해 memo에 넣으므로 원문 행 1개가 SDK 목록 2개가 됩니다. 공유 게시물
 * 안의 원본은 이 채널의 행이 아니므로 목록에서 빼고 행 수를 대조합니다.
 * @template {object[]} T
 * @param {CollectionChannel | CollectionContinuation} feed @param {T} posts @returns {T} */
function topLevelPosts(feed, posts) {
  /** @type {Set<unknown>} */
  const embedded = new Set(feed.memo.getType(YTNodes.SharedPost).map((shared) => shared.original_post));
  return embedded.size === 0 ? posts : /** @type {T} */ (posts.filter((post) => !embedded.has(post)));
}

/** SDK Feed.getContinuation과 같은 endpoint 호출을 유지하면서 원문을 보존합니다.
 * @param {CollectionChannel | CollectionContinuation} feed */
async function loadContinuation(feed) {
  return runUpstream(async () => {
    throwIfAborted();
    const source = evidence.get(feed);
    if (!source) throw drift("channel tab evidence is unavailable");
    const headerContinuations = feed.page.header_memo?.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView) ?? [];
    const continuation = feed.memo.getType(YTNodes.ContinuationItem, YTNodes.ContinuationItemView)
      .find((candidate) => !headerContinuations.includes(candidate));
    if (continuation === undefined) throw drift("pagination continuation is missing");
    const response = await continuation.endpoint.call(feed.actions);
    throwIfAborted();
    const raw = response.data;
    const parsed = readUpstream(() => Parser.parseResponse(raw));
    return new CollectionContinuation(feed.actions, parsed, raw, source.channelId, continuationRowCount(raw));
  });
}

/** @param {Record<string, unknown>} selected */
function bodyRowCount(selected) {
  const content = recordOf(selected.content);
  const body = recordOf(content?.sectionListRenderer ?? content?.richGridRenderer);
  const rows = leafRowsOf(body?.contents);
  if (rows === null) throw drift("requested channel tab body is not recognized");
  return rows.length;
}

/** @param {RawPage} raw */
function continuationRowCount(raw) {
  const actions = raw.onResponseReceivedActions ?? raw.onResponseReceivedEndpoints ?? raw.onResponseReceivedCommands;
  if (!Array.isArray(actions) || actions.length === 0) throw drift("continuation shape is not recognized");
  let count = 0;
  for (const action of actions) {
    const command = recordOf(recordOf(action)?.appendContinuationItemsAction ?? recordOf(action)?.reloadContinuationItemsCommand);
    const rows = leafRowsOf(command?.continuationItems);
    if (rows === null) throw drift("continuation shape is not recognized");
    count += rows.length;
  }
  return count;
}

/** @param {RawPage} raw @param {string} channelId @param {string} tab @param {boolean} [loaded] */
function inspectRawTab(raw, channelId, tab, loaded = false) {
  const tabs = !Array.isArray(raw?.contents) ? raw?.contents?.twoColumnBrowseResultsRenderer?.tabs : undefined;
  if (!Array.isArray(tabs) || tabs.length === 0) throw drift("channel tab list is not recognized");
  const returnedId = raw.metadata?.channelMetadataRenderer?.externalId;
  if (returnedId !== undefined && returnedId !== channelId) {
    throw drift("channel identity does not match the request");
  }
  const entries = tabs.map(entry => {
    if (!isRecord(entry) || Object.hasOwn(entry, "tabRenderer") === Object.hasOwn(entry, "expandableTabRenderer")) {
      throw drift("channel tab list is not recognized");
    }
    const renderer = entry.tabRenderer ?? entry.expandableTabRenderer;
    if (!isRecord(renderer) || (renderer.selected !== undefined && typeof renderer.selected !== "boolean")) {
      throw drift("channel tab list is not recognized");
    }
    const endpoint = recordOf(renderer.endpoint);
    const route = routeOf(recordOf(recordOf(endpoint?.commandMetadata)?.webCommandMetadata)?.url);
    return { renderer, route, browseId: recordOf(endpoint?.browseEndpoint)?.browseId };
  });
  if (entries.filter(entry => entry.renderer.selected === true).length > 1) {
    throw drift("requested channel tab is not selected uniquely");
  }
  const targets = entries.filter(entry => entry.route?.tab === tab);
  if (targets.length === 0) {
    // 부재를 확정하려면 원문의 모든 슬롯과 채널 소유권을 식별할 수 있어야 합니다.
    if (loaded || entries.some(entry => entry.route === undefined ||
        (entry.route.channelId !== undefined && entry.route.channelId !== channelId) ||
        (entry.browseId !== undefined && entry.browseId !== channelId))) {
      throw drift("channel tab list is not recognized");
    }
    return null;
  }
  if (targets.length !== 1) throw drift("requested channel tab is ambiguous");
  if (targets[0].route.channelId !== undefined && targets[0].route.channelId !== channelId) {
    throw drift("channel tab identity does not match the request");
  }
  const target = targets[0].renderer;
  if (targets[0].browseId !== channelId) {
    throw drift("channel tab identity does not match the request");
  }
  if (loaded || target.selected === true) {
    if (target.selected !== true || entries.filter(entry => entry.renderer?.selected === true).length !== 1) {
      throw drift("requested channel tab is not selected uniquely");
    }
    bodyRowCount(target);
  }
  return target;
}

/** 선택 본문의 명시적 행 목록만 검사하며, 일반 renderer 객체 내부는 SDK가 해석합니다.
 * continuation 항목은 행이 아니므로 제외한 leaf 행을 돌려주고, 인식하지 못한 shape은 null입니다.
 * @param {unknown} contents
 * @returns {Record<string, unknown>[] | null}
 */
function leafRowsOf(contents) {
  if (!Array.isArray(contents)) return null;
  const rows = [];
  const lists = [contents];
  while (lists.length > 0) {
    for (const entry of lists.pop()) {
      if (!isRecord(entry) || Object.keys(entry).length === 0) return null;
      if (Object.hasOwn(entry, "itemSectionRenderer")) {
        const section = entry.itemSectionRenderer;
        if (!isRecord(section) || !Array.isArray(section.contents)) return null;
        lists.push(section.contents);
      } else if (!Object.hasOwn(entry, "continuationItemRenderer")) {
        rows.push(entry);
      }
    }
  }
  return rows;
}

/** @param {unknown} value */
function routeOf(value) {
  if (typeof value !== "string" || value.trim() === "" || (!value.startsWith("/") && !/^https?:\/\//u.test(value))) return undefined;
  try {
    const url = new URL(value, "https://www.youtube.com");
    if (!["https:", "http:"].includes(url.protocol) || !["www.youtube.com", "youtube.com", "m.youtube.com"].includes(url.hostname) ||
        url.username !== "" || url.password !== "" || url.port !== "") return undefined;
    const segments = url.pathname.replace(/\/+$/u, "").split("/").slice(1).map(decodeURIComponent);
    if (segments.some(segment => segment === "" || segment.includes("/"))) return undefined;
    if (segments[0] === "channel" && segments.length >= 2 && segments.length <= 3) {
      return { tab: segments[2] ?? "featured", channelId: segments[1] };
    }
    if (segments[0]?.startsWith("@") && segments[0].length > 1 && segments.length <= 2) {
      return { tab: segments[1] ?? "featured" };
    }
    if (["c", "user"].includes(segments[0]) && segments.length >= 2 && segments.length <= 3) {
      return { tab: segments[2] ?? "featured" };
    }
    return undefined;
  } catch {
    return undefined;
  }
}

/** @param {unknown} value */
function recordOf(value) { return isRecord(value) ? value : undefined; }
/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) { return value !== null && typeof value === "object" && !Array.isArray(value); }
/** @param {string} message */
function drift(message) { return Object.assign(new Error(message), { code: "parser_drift" }); }
function throwIfAborted() {
  if (currentRequestSignal()?.aborted) throw new DOMException("aborted", "AbortError");
}
