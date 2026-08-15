// @ts-check
import {
  channelEndpoint,
  communityEndpoint,
  contentEndpoint,
  handleRpcRequest,
  viewerEndpoint,
} from "./rpc-validation.mjs";

/** @type {import("./contracts.d.ts").ProxyConfigurator} */
const ignoreProxy = () => {};

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").CommunityFetcher} fetchCommunity
 * @param {import("./contracts.d.ts").ProxyConfigurator} [configureProxy]
 */
export async function handleCommunityRequest(rawBody, fetchCommunity, configureProxy = ignoreProxy) {
  return handleRpcRequest(rawBody, communityEndpoint, async (payload) => {
    configureProxy(payload.proxy_url);
    return fetchCommunity({
      channelId: payload.channel_id,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    });
  });
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ContentFetcher} fetchContent
 * @param {import("./contracts.d.ts").ProxyConfigurator} [configureProxy]
 */
export async function handleContentRequest(rawBody, fetchContent, configureProxy = ignoreProxy) {
  return handleRpcRequest(rawBody, contentEndpoint, async (payload) => {
    configureProxy(payload.proxy_url);
    return fetchContent({
      channelId: payload.channel_id,
      kind: payload.kind,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    });
  });
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ChannelFetcher} fetchChannel
 * @param {import("./contracts.d.ts").ProxyConfigurator} [configureProxy]
 */
export async function handleChannelRequest(rawBody, fetchChannel, configureProxy = ignoreProxy) {
  return handleRpcRequest(rawBody, channelEndpoint, async (payload) => {
    configureProxy(payload.proxy_url);
    return fetchChannel({
      channelId: payload.channel_id,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    });
  });
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ViewerFetcher} fetchViewer
 * @param {import("./contracts.d.ts").ProxyConfigurator} [configureProxy]
 */
export async function handleViewerRequest(rawBody, fetchViewer, configureProxy = ignoreProxy) {
  return handleRpcRequest(rawBody, viewerEndpoint, async (payload) => {
    configureProxy(payload.proxy_url);
    return fetchViewer({
      videoId: payload.video_id,
      maxAggregateBytes: payload.max_aggregate_bytes,
    });
  });
}
