import type { ContentFetchOptions, ContentResult } from "./contracts.d.ts";

export function fetchContentFeed(
  options: ContentFetchOptions & {
    innertube?: unknown;
  },
): Promise<ContentResult>;
