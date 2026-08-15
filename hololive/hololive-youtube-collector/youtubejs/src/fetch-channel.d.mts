import type { ChannelFetchOptions, ChannelResult } from "./contracts.d.ts";

export function fetchChannelFeed(
  options: ChannelFetchOptions & {
    innertube?: unknown;
  },
): Promise<ChannelResult>;
