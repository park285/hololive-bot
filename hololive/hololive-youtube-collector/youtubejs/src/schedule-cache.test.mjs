import assert from "node:assert/strict";
import test from "node:test";

import { restrictedReuseMs, ScheduleLookupCache } from "./schedule-cache.mjs";

const minute = 60_000;
const start = Date.parse("2026-09-01T00:00:00Z");

function clock() {
  let now = start;
  return { now: () => now, advance: (ms) => { now += ms; } };
}

function upcoming(at) {
  return { isLive: false, isUpcoming: true, startTimestamp: new Date(at).toISOString() };
}

test("a far-future schedule is reused for at most an hour", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.remember("UC_A", "v1", upcoming(start + 10 * 60 * minute));
  time.advance(60 * minute - 1);
  assert.deepEqual(cache.reuse("UC_A", "v1"), upcoming(start + 10 * 60 * minute));
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "v1"), undefined);
  assert.equal(cache.size, 0);
});

test("reuse lasts a sixth of the remaining wait and ends before the near-start window", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.remember("UC_A", "two-hours", upcoming(start + 120 * minute));
  cache.remember("UC_A", "thirty-one-minutes", upcoming(start + 31 * minute));
  time.advance(minute - 1);
  assert.notEqual(cache.reuse("UC_A", "thirty-one-minutes"), undefined);
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "thirty-one-minutes"), undefined);
  time.advance(19 * minute - 1);
  assert.notEqual(cache.reuse("UC_A", "two-hours"), undefined);
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "two-hours"), undefined);
});

test("near-start, live, unscheduled, and past schedules are never stored", () => {
  const cache = new ScheduleLookupCache({ now: () => start });
  cache.remember("UC_A", "near", upcoming(start + 30 * minute));
  cache.remember("UC_A", "past", upcoming(start - minute));
  cache.remember("UC_A", "live", { isLive: true, isUpcoming: false, startTimestamp: new Date(start).toISOString() });
  cache.remember("UC_A", "not-upcoming", { isLive: false, isUpcoming: false, startTimestamp: new Date(start + 600 * minute).toISOString() });
  cache.remember("UC_A", "no-time", { isLive: false, isUpcoming: true, startTimestamp: null });
  assert.equal(cache.size, 0);
});

test("a restriction is reused for a fixed period without a schedule", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.remember("UC_A", "members", { scheduleUnavailableReason: "access_restricted" });
  time.advance(restrictedReuseMs - 1);
  assert.deepEqual(cache.reuse("UC_A", "members"), { scheduleUnavailableReason: "access_restricted" });
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "members"), undefined);
});

test("a newer unreusable result replaces the stored one and entries are scoped by channel", () => {
  const cache = new ScheduleLookupCache({ now: () => start });
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute));
  assert.equal(cache.reuse("UC_B", "v1"), undefined);
  cache.remember("UC_A", "v1", { isLive: true, isUpcoming: false, startTimestamp: null });
  assert.equal(cache.reuse("UC_A", "v1"), undefined);
});

test("returned results are copies and the oldest entry is evicted past the size bound", () => {
  const cache = new ScheduleLookupCache({ maxEntries: 2, now: () => start });
  for (const id of ["v1", "v2", "v3"]) cache.remember("UC_A", id, upcoming(start + 600 * minute));
  assert.equal(cache.size, 2);
  assert.equal(cache.reuse("UC_A", "v1"), undefined);
  const reused = cache.reuse("UC_A", "v2");
  reused.startTimestamp = "mutated";
  assert.equal(cache.reuse("UC_A", "v2").startTimestamp, new Date(start + 600 * minute).toISOString());
  assert.throws(() => new ScheduleLookupCache({ maxEntries: 0 }), TypeError);
});
