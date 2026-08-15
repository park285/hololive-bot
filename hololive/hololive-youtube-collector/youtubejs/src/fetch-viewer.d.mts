import type { ViewerFetchOptions, ViewerResult } from "./contracts.d.ts";

export function fetchViewerFeed(
  options: ViewerFetchOptions & {
    innertube?: unknown;
  },
): Promise<ViewerResult>;
