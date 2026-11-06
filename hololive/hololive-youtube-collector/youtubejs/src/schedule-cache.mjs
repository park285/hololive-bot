// @ts-check

const minute = 60_000;
// 시작이 가까운 예정 영상은 조기 시작·LIVE 전환·일정 변경을 매 poll의 player로 확인합니다.
export const scheduleReuseNearStartMs = 30 * minute;
const scheduleReuseMinMs = 5 * minute;
// 재사용 중에는 canonical schedule_observed_at이 나아가지 않습니다. 알람 checker(UPCOMING 관측 15분 이내)와
// 생일 방송 조회(30분 이내)의 최신성 창 안에 fleet의 마지막 실제 확인이 남도록, poll 간격을 더해도 15분보다 짧게 둡니다.
// 앞당겨진 일정을 이 AP가 늦게 보는 최대 기간이기도 합니다. 다른 AP·provider의 새 관측은 원래 슬롯 비교로 되돌려지지 않습니다.
export const scheduleReuseMaxMs = 10 * minute;
const scheduleReuseDivisor = 6;
const defaultMaxEntries = 512;

/**
 * @typedef {{ isLive?: boolean, isUpcoming?: boolean, startTimestamp?: string | null, scheduleUnavailableReason?: string }} ScheduleMetadata
 * @typedef {{ startTimestamp: string, observedAt: number, reuseUntil: number }} ScheduleEntry
 * @typedef {{ isLive: false, isUpcoming: true, startTimestamp: string, observedAt: string }} ReusedSchedule
 */

/**
 * 시각 없는 예정 영상의 player 예정 시각을 helper 프로세스 안에서 재사용합니다.
 * 항목은 확인한 요청의 collector lease 슬롯 시각(`observedAt`)을 함께 보관해, 발행 행이 이번 poll이 아닌
 * 그 슬롯의 관측임을 API와 같은 시계로 밝힙니다. 재사용 기한은 helper 벽시계로 계산합니다.
 * 접근 제한·LIVE 판정·오류는 저장하지 않습니다. 재시작하면 비며 AP마다 따로 유지됩니다.
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
   * 유효하고 `observedBefore`보다 앞선 슬롯에서 확인한 예정 시각이 있으면 돌려줍니다.
   * 같은 슬롯의 재시도나 겹친 요청은 재사용하지 않아, 재사용 행의 관측 시각이 항상 이번 관측보다 앞섭니다.
   * @param {string} channelId
   * @param {string} videoId
   * @param {number} observedBefore
   * @returns {ReusedSchedule | undefined}
   */
  reuse(channelId, videoId, observedBefore) {
    const key = cacheKey(channelId, videoId);
    const entry = this.#entries.get(key);
    if (entry === undefined) return undefined;
    if (this.#now() >= entry.reuseUntil) {
      this.#entries.delete(key);
      return undefined;
    }
    if (entry.observedAt >= observedBefore) return undefined;
    return {
      isLive: false,
      isUpcoming: true,
      startTimestamp: entry.startTimestamp,
      observedAt: new Date(entry.observedAt).toISOString(),
    };
  }

  /**
   * `observedAt` 슬롯에서 player로 새로 확인한 결과를 기록합니다. 재사용할 수 없는 결과는 기존 항목을 지웁니다.
   * 더 늦은 슬롯의 항목은 늦게 끝난 앞 슬롯의 조회가 덮어쓰지 않습니다.
   * @param {string} channelId
   * @param {string} videoId
   * @param {ScheduleMetadata} metadata
   * @param {number} observedAt
   */
  remember(channelId, videoId, metadata, observedAt) {
    const key = cacheKey(channelId, videoId);
    const existing = this.#entries.get(key);
    if (existing !== undefined && existing.observedAt > observedAt) return;
    this.#entries.delete(key);
    const reuseUntil = reuseDeadline(metadata, this.#now());
    if (reuseUntil === undefined || typeof metadata.startTimestamp !== "string") return;
    this.#entries.set(key, { startTimestamp: metadata.startTimestamp, observedAt, reuseUntil });
    // 만료 기한이 상한 10분이라 삽입 순서 제거로 충분합니다.
    while (this.#entries.size > this.#maxEntries) {
      const oldest = this.#entries.keys().next().value;
      if (oldest === undefined) break;
      this.#entries.delete(oldest);
    }
  }

  /**
   * 목록이 시각을 직접 보이거나 다른 상태로 바뀐 영상은 다음 미확정 관측에서 player로 다시 확인합니다.
   * @param {string} channelId
   * @param {string} videoId
   */
  forget(channelId, videoId) {
    this.#entries.delete(cacheKey(channelId, videoId));
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
  if (metadata.scheduleUnavailableReason !== undefined || metadata.isLive === true ||
      metadata.isUpcoming !== true || typeof metadata.startTimestamp !== "string") {
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
