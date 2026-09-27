export interface RawLiveMetadata {
  videoId: string;
  /** 부가 사실입니다. 형식이 맞을 때만 존재하며 기존 판정에는 쓰지 않습니다. */
  channelId?: string;
  isLive?: boolean;
  /** 부가 사실입니다. raw boolean일 때만 존재합니다. */
  isLiveNow?: boolean;
  isUpcoming?: boolean;
  isLiveContent?: boolean;
  startTimestamp?: string;
  /** 부가 사실입니다. 유효한 RFC3339일 때만 정규화해 존재합니다. */
  endTimestamp?: string;
  scheduleUnavailableReason?: "access_restricted";
}

export function fetchLiveMetadata(innertube: unknown, videoId: string): Promise<RawLiveMetadata>;
export function parseRawLiveMetadata(raw: unknown, expectedVideoId: string): RawLiveMetadata;
