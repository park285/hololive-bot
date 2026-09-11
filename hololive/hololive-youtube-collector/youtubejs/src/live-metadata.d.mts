export interface RawLiveMetadata {
  videoId: string;
  isLive?: boolean;
  isUpcoming?: boolean;
  isLiveContent?: boolean;
  startTimestamp?: string;
  scheduleUnavailableReason?: "access_restricted";
}

export function fetchLiveMetadata(innertube: unknown, videoId: string): Promise<RawLiveMetadata>;
export function parseRawLiveMetadata(raw: unknown, expectedVideoId: string): RawLiveMetadata;
