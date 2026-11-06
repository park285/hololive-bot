// @ts-check

const minute = 60_000;
// 시작이 가까운 예정 영상은 조기 시작·LIVE 전환·일정 변경을 매 poll의 player로 확인합니다.
export const scheduleReuseNearStartMs = 30 * minute;
const scheduleReuseMinMs = 5 * minute;
const scheduleReuseMaxMs = 60 * minute;
// 남은 시간의 1/6만 재사용해 앞당긴 일정의 감지 지연이 남은 대기 시간보다 충분히 짧게 합니다.
const scheduleReuseDivisor = 6;
// 접근 제한은 시각이 없으므로 고정 기간만 재사용합니다.
export const restrictedReuseMs = 30 * minute;
const defaultMaxEntries = 512;

/**
 * @typedef {{ scheduleUnavailableReason?: string, isLive?: boolean, isUpcoming?: boolean, startTimestamp?: string | null }} ScheduleMetadata
 * @typedef {{ metadata: ScheduleMetadata, reuseUntil: number }} ScheduleEntry
 */

/**
 * 시각 없는 예정 영상의 player 일정 확인 결과를 helper 프로세스 안에서 재사용합니다.
 * streams 탭이 같은 영상을 여전히 시각 없는 UPCOMING으로 보일 때만 호출되며, LIVE 판정과 오류는 저장하지 않습니다.
 * 재시작하면 비며, AP마다 따로 유지됩니다.
 */
export class ScheduleLookupCache {
  /** @type {Map<string, ScheduleEntry>} */
  #entries = new Map();
  #maxEntries;
  #now;

  /** @param {{ maxEntries?: number, now?: () => number }} [options] */
  constructor({ maxEntries = defaultMaxEntries, now = Date.now } = {}) {
    if (!Number.isSafeInteger(maxEntries) || maxEntries < 1) {
      throw new TypeError("schedule cache size must be a positive integer");
    }
    this.#maxEntries = maxEntries;
    this.#now = now;
  }

  /**
   * 유효한 확인 결과가 있으면 player 응답과 같은 형태로 돌려줍니다.
   * @param {string} channelId
   * @param {string} videoId
   * @returns {ScheduleMetadata | undefined}
   */
  reuse(channelId, videoId) {
    const key = cacheKey(channelId, videoId);
    const entry = this.#entries.get(key);
    if (entry === undefined) return undefined;
    if (this.#now() >= entry.reuseUntil) {
      this.#entries.delete(key);
      return undefined;
    }
    return { ...entry.metadata };
  }

  /**
   * 새로 받은 player 결과 중 재사용할 수 있는 예정 시각과 접근 제한만 저장합니다.
   * @param {string} channelId
   * @param {string} videoId
   * @param {ScheduleMetadata} metadata
   */
  remember(channelId, videoId, metadata) {
    const key = cacheKey(channelId, videoId);
    this.#entries.delete(key);
    const reuseUntil = reuseDeadline(metadata, this.#now());
    if (reuseUntil === undefined) return;
    const stored = metadata.scheduleUnavailableReason === "access_restricted"
      ? { scheduleUnavailableReason: "access_restricted" }
      : { isLive: false, isUpcoming: true, startTimestamp: metadata.startTimestamp };
    this.#entries.set(key, { metadata: stored, reuseUntil });
    // 만료 기한이 상한 60분이라 삽입 순서 제거로 충분합니다.
    while (this.#entries.size > this.#maxEntries) {
      const oldest = this.#entries.keys().next().value;
      if (oldest === undefined) break;
      this.#entries.delete(oldest);
    }
  }

  get size() {
    return this.#entries.size;
  }
}

/**
 * @param {ScheduleMetadata} metadata
 * @param {number} now
 * @returns {number | undefined}
 */
function reuseDeadline(metadata, now) {
  if (metadata.scheduleUnavailableReason === "access_restricted") {
    return now + restrictedReuseMs;
  }
  if (metadata.isLive === true || metadata.isUpcoming !== true || typeof metadata.startTimestamp !== "string") {
    return undefined;
  }
  const scheduledAt = Date.parse(metadata.startTimestamp);
  if (!Number.isFinite(scheduledAt)) return undefined;
  const remaining = scheduledAt - now;
  const reuseFor = Math.min(scheduleReuseMaxMs, Math.max(scheduleReuseMinMs, remaining / scheduleReuseDivisor));
  // 재사용 기한은 시작 임박 구간에 들어가기 전에 끝납니다.
  const reuseUntil = Math.min(now + reuseFor, scheduledAt - scheduleReuseNearStartMs);
  return reuseUntil > now ? reuseUntil : undefined;
}

/**
 * @param {string} channelId
 * @param {string} videoId
 */
function cacheKey(channelId, videoId) {
  return `${channelId}\u0000${videoId}`;
}
