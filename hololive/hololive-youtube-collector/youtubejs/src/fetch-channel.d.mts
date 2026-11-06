import type { ChannelFetchOptions, ChannelResult } from "./contracts.d.ts";
import type { ScheduleLookupCache } from "./schedule-cache.mjs";

export function fetchChannelFeed(
  options: ChannelFetchOptions & {
    innertube?: unknown;
    scheduleCache?: ScheduleLookupCache;
  },
): Promise<Omit<ChannelResult, "protocol_version">>;
