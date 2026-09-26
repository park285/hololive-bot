// @ts-check
import {
  channelEndpoint,
  channelLiveCheckEndpoint,
  communityEndpoint,
  contentEndpoint,
  handleRpcRequest,
  RpcResponseError,
  videoLiveCheckEndpoint,
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
      kind: payload.kind,
      maxPages: payload.max_pages,
      maxSuccessResponseBytes: payload.max_success_response_bytes,
    });
  }, maximumSuccessResponseBytes);
}

/**
 * 채널 /live 확인 RPC입니다. 결과 subject가 요청 채널과 다르면 관측으로 쓰지 않고 계약 위반으로 거부합니다.
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
 * 영상 상태 확인 RPC입니다. identity 확인 여부와 무관하게 결과 video_id는 요청 subject와 같아야 합니다.
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
