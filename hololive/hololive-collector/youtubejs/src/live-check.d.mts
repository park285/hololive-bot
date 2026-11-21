import type { ChannelLiveCheckResult, VideoLiveCheckResult } from "./contracts.d.ts";
import type { Innertube } from "youtubei.js";
import type { ProofController } from "./proof-controller.mjs";

export type ChannelLiveCheckObservation = Omit<ChannelLiveCheckResult, "protocol_version">;
export type VideoLiveCheckObservation = Omit<VideoLiveCheckResult, "protocol_version">;

export class LiveCheckRequestError extends Error {
  name: "LiveCheckRequestError";
  code: "collection_failed";
  failureClass: "TRANSIENT";
  constructor(message: string, options?: { cause?: unknown });
}

export function createLiveCheckInnertube(options?: { fetchImpl?: unknown }): Promise<Innertube>;
export function fetchChannelLiveCheck(
  innertube: unknown,
  channelId: string,
  clock?: () => number,
  proof?: ProofController,
): Promise<ChannelLiveCheckObservation>;
export function fetchVideoLiveCheck(
  innertube: unknown,
  videoId: string,
  clock?: () => number,
  proof?: ProofController,
): Promise<VideoLiveCheckObservation>;
export function classifyResolvedChannel(
  raw: unknown,
  channelId: string,
): { kind: "result"; result: ChannelLiveCheckObservation } | { kind: "watch"; videoId: string };
export function classifyChannelPlayer(
  raw: unknown,
  channelId: string,
  selectedVideoId: string,
  nowMs: number,
): ChannelLiveCheckObservation;
export function parseVideoLiveCheck(raw: unknown, videoId: string, nowMs: number): VideoLiveCheckObservation;
