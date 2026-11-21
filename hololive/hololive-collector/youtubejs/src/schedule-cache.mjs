// @ts-check

const minute = 60_000;
export const scheduleReuseNearStartMs = 30 * minute;
const scheduleReuseMinMs = 5 * minute;
export const scheduleReuseMaxMs = 10 * minute;
const scheduleReuseDivisor = 6;
const defaultMaxEntries = 512;

/**
 * @typedef {{ isLive?: boolean, isUpcoming?: boolean, startTimestamp?: string | null, scheduleUnavailableReason?: string }} ScheduleMetadata
 * @typedef {{ startTimestamp: string, observedAt: number, reuseUntil: number }} ScheduleEntry
 * @typedef {{ isLive: false, isUpcoming: true, startTimestamp: string, observedAt: string }} ReusedSchedule
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
   * @param {string} channelId
   * @param {number} observedAt
   */
  beginSlot(channelId, observedAt) {
    const latest = this.#channelSlots.get(channelId);
    if (latest !== undefined && latest >= observedAt) return;
    this.#channelSlots.delete(channelId);
    this.#channelSlots.set(channelId, observedAt);
    evictOldest(this.#channelSlots, this.#maxEntries);
  }

  /**
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
   * @param {string} channelId
   * @param {string} videoId
   * @param {ScheduleMetadata} metadata
   * @param {number} observedAt
   */
  remember(channelId, videoId, metadata, observedAt) {
    const latestSlot = this.#channelSlots.get(channelId);
    if (latestSlot === undefined || latestSlot > observedAt) return;
    const key = cacheKey(channelId, videoId);
    if ((this.#entries.get(key)?.observedAt ?? -Infinity) > observedAt) return;
    this.#entries.delete(key);
    const reuseUntil = reuseDeadline(metadata, this.#now());
    if (reuseUntil === undefined || typeof metadata.startTimestamp !== "string") return;
    this.#entries.set(key, { startTimestamp: metadata.startTimestamp, observedAt, reuseUntil });
    evictOldest(this.#entries, this.#maxEntries);
  }

  /**
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
