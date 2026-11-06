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
 * 채널마다 시작한 가장 늦은 슬롯을 따로 기록해, 겹친 요청에서 늦게 끝난 앞 슬롯의 조회가 그 뒤 슬롯의
 * 무효화나 재확인을 되돌리지 못하게 합니다. 접근 제한·LIVE 판정·오류는 저장하지 않습니다.
 * 재시작하면 비며 AP마다 따로 유지됩니다.
 */
export class ScheduleLookupCache {
  /** @type {Map<string, ScheduleEntry>} */
  #entries = new Map();
  /** @type {Map<string, number>} */
  #channelSlots = new Map();
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
   * 채널의 `observedAt` 슬롯 수집이 시작됐음을 기록합니다. 그보다 앞 슬롯의 조회 결과는 이후 저장하지 않습니다.
   * 겹친 요청이 드물어 앞 슬롯 결과를 버리는 비용은 다음 poll의 조회 한 번뿐입니다.
   * @param {string} channelId
   * @param {number} observedAt
   */
  beginSlot(channelId, observedAt) {
    const latest = this.#channelSlots.get(channelId);
    if (latest !== undefined && latest >= observedAt) return;
    this.#channelSlots.delete(channelId);
    this.#channelSlots.set(channelId, observedAt);
    // 채널 수가 상한보다 훨씬 적어 축출은 사실상 없고, 일어나도 그 채널의 겹친 요청 보호만 잃습니다.
    // 그때도 재사용 행은 원래 슬롯 시각을 실어 API가 더 새 일정을 지킵니다.
    evictOldest(this.#channelSlots, this.#maxEntries);
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
   * 채널의 더 늦은 슬롯이 이미 시작했거나 더 늦은 슬롯의 항목이 있으면 기록하지 않습니다.
   * @param {string} channelId
   * @param {string} videoId
   * @param {ScheduleMetadata} metadata
   * @param {number} observedAt
   */
  remember(channelId, videoId, metadata, observedAt) {
    if ((this.#channelSlots.get(channelId) ?? -Infinity) > observedAt) return;
    const key = cacheKey(channelId, videoId);
    if ((this.#entries.get(key)?.observedAt ?? -Infinity) > observedAt) return;
    this.#entries.delete(key);
    const reuseUntil = reuseDeadline(metadata, this.#now());
    if (reuseUntil === undefined || typeof metadata.startTimestamp !== "string") return;
    this.#entries.set(key, { startTimestamp: metadata.startTimestamp, observedAt, reuseUntil });
    // 만료 기한이 상한 10분이라 삽입 순서 제거로 충분합니다.
    evictOldest(this.#entries, this.#maxEntries);
  }

  /**
   * `observedAt` 슬롯 이전에 확인한 항목을 지웁니다. 목록이 시각이나 다른 상태를 직접 보인 영상과,
   * player로 다시 확인하기 직전의 영상에 씁니다. 그래서 재확인이 실패하면 다음 슬롯도 다시 조회합니다.
   * @param {string} channelId
   * @param {string} videoId
   * @param {number} observedAt
   */
  forget(channelId, videoId, observedAt) {
    const key = cacheKey(channelId, videoId);
    if ((this.#entries.get(key)?.observedAt ?? Infinity) <= observedAt) this.#entries.delete(key);
  }

  get size() {
    return this.#entries.size;
  }
}

/**
 * @template V
 * @param {Map<string, V>} map
 * @param {number} maxEntries
 */
function evictOldest(map, maxEntries) {
  while (map.size > maxEntries) {
    const oldest = map.keys().next().value;
    if (oldest === undefined) break;
    map.delete(oldest);
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
