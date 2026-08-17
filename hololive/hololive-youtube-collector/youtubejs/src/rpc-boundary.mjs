// @ts-check
import {
  channelEndpoint,
  communityEndpoint,
  contentEndpoint,
  handleRpcRequest,
  viewerEndpoint,
} from "./rpc-validation.mjs";

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").CommunityFetcher} fetchCommunity
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleCommunityRequest(rawBody, fetchCommunity, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, communityEndpoint, async (payload) => {
    return fetchCommunity({
      channelId: payload.channel_id,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ContentFetcher} fetchContent
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleContentRequest(rawBody, fetchContent, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, contentEndpoint, async (payload) => {
    return fetchContent({
      channelId: payload.channel_id,
      kind: payload.kind,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ChannelFetcher} fetchChannel
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleChannelRequest(rawBody, fetchChannel, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, channelEndpoint, async (payload) => {
    return fetchChannel({
      channelId: payload.channel_id,
      maxPages: payload.max_pages,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ViewerFetcher} fetchViewer
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleViewerRequest(rawBody, fetchViewer, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, viewerEndpoint, async (payload) => {
    return fetchViewer({
      videoId: payload.video_id,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
}
