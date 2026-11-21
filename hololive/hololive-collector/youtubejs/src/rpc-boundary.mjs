// @ts-check
import { channelEndpoint, communityEndpoint, contentEndpoint } from "./rpc-collection-validation.mjs";
import { channelLiveCheckEndpoint, videoLiveCheckEndpoint } from "./rpc-live-check-validation.mjs";
import { handleRpcRequest } from "./rpc-execution.mjs";
import { currentScheduleLookups } from "./request-context.mjs";
import { RpcResponseError } from "./rpc-errors.mjs";

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
  const result = await handleRpcRequest(rawBody, channelEndpoint, async (payload) => {
    return fetchChannel({
      channelId: payload.channel_id,
      kind: payload.kind,
      ...(payload.max_schedule_lookups === undefined ? {} : { maxScheduleLookups: payload.max_schedule_lookups }),
      ...(payload.scheduled_for === undefined ? {} : { scheduledFor: payload.scheduled_for }),
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
  const lookups = currentScheduleLookups();
  if (result.status !== 200 && lookups > 0) {
    return { status: result.status, body: { ...result.body, schedule_lookups: lookups } };
  }
  return result;
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").ChannelLiveCheckFetcher} fetchChannelLiveCheck
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleChannelLiveCheckRequest(rawBody, fetchChannelLiveCheck, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, channelLiveCheckEndpoint, async (payload) => {
    const result = await fetchChannelLiveCheck({
      channelId: payload.channel_id,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
    if (result?.channel_id !== payload.channel_id) {
      throw new RpcResponseError("channel live check subject does not match the request");
    }
    return result;
  }, maximumSuccessResponseBytes);
}

/**
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").VideoLiveCheckFetcher} fetchVideoLiveCheck
 * @param {number} [maximumSuccessResponseBytes]
 */
export async function handleVideoLiveCheckRequest(rawBody, fetchVideoLiveCheck, maximumSuccessResponseBytes) {
  return handleRpcRequest(rawBody, videoLiveCheckEndpoint, async (payload) => {
    const result = await fetchVideoLiveCheck({
      videoId: payload.video_id,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
    if (result?.video_id !== payload.video_id) {
      throw new RpcResponseError("video live check subject does not match the request");
    }
    return result;
  }, maximumSuccessResponseBytes);
}
