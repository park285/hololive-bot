import type { CommunityFetchOptions, CommunityResult } from "./contracts.d.ts";
import type { InnertubeFetch } from "./upstream-feeds.d.ts";

export function fetchCommunityFeed(
  options: CommunityFetchOptions & {
    innertube?: unknown;
    postType?: unknown;
  },
): Promise<CommunityResult>;

export function createInnertube(options?: {
  fetchImpl?: InnertubeFetch;
}): Promise<unknown>;

export function emptyCommunityPage(): CommunityResult;
