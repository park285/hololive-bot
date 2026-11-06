import assert from "node:assert/strict";
import test from "node:test";

import { ScheduleLookupCache } from "./schedule-cache.mjs";

const minute = 60_000;
const start = Date.parse("2026-09-01T00:00:00Z");

function clock() {
  let now = start;
  return { now: () => now, advance: (ms) => { now += ms; } };
}

function upcoming(at) {
  return { isLive: false, isUpcoming: true, startTimestamp: new Date(at).toISOString() };
}

test("a reused schedule carries the slot it was confirmed in and lasts at most ten minutes", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.remember("UC_A", "v1", upcoming(start + 10 * 60 * minute), start);
  time.advance(10 * minute - 1);
  assert.deepEqual(cache.reuse("UC_A", "v1", time.now()), {
    ...upcoming(start + 10 * 60 * minute), observedAt: new Date(start).toISOString(),
  });
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "v1", time.now()), undefined);
  assert.equal(cache.size, 0);
});

test("reuse lasts a sixth of the remaining wait and ends before the near-start window", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.remember("UC_A", "fifty-four-minutes", upcoming(start + 54 * minute), start);
  cache.remember("UC_A", "thirty-one-minutes", upcoming(start + 31 * minute), start);
  time.advance(minute - 1);
  assert.notEqual(cache.reuse("UC_A", "thirty-one-minutes", time.now()), undefined);
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "thirty-one-minutes", time.now()), undefined);
  time.advance(8 * minute - 1);
  assert.notEqual(cache.reuse("UC_A", "fifty-four-minutes", time.now()), undefined);
  time.advance(1);
  assert.equal(cache.reuse("UC_A", "fifty-four-minutes", time.now()), undefined);
});

test("near-start, live, unscheduled, past, and restricted results are never stored", () => {
  const cache = new ScheduleLookupCache({ now: () => start });
  cache.remember("UC_A", "near", upcoming(start + 30 * minute), start);
  cache.remember("UC_A", "past", upcoming(start - minute), start);
  cache.remember("UC_A", "live", { isLive: true, isUpcoming: false, startTimestamp: new Date(start).toISOString() }, start);
  cache.remember("UC_A", "not-upcoming", { isLive: false, isUpcoming: false, startTimestamp: new Date(start + 600 * minute).toISOString() }, start);
  cache.remember("UC_A", "no-time", { isLive: false, isUpcoming: true, startTimestamp: null }, start);
  cache.remember("UC_A", "members", { scheduleUnavailableReason: "access_restricted" }, start);
  assert.equal(cache.size, 0);
});

test("an entry stored after a request started is not reused by that request", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  const requestStartedAt = time.now();
  time.advance(1000);
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute), time.now());
  assert.equal(cache.reuse("UC_A", "v1", requestStartedAt), undefined);
  assert.notEqual(cache.reuse("UC_A", "v1", time.now() + 1), undefined);
});

test("a slower older lookup never replaces a newer one, but a newer unreusable result clears it", () => {
  const cache = new ScheduleLookupCache({ now: () => start + 2 * minute });
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute), start + minute);
  cache.remember("UC_A", "v1", upcoming(start + 300 * minute), start);
  assert.equal(cache.reuse("UC_A", "v1", start + 3 * minute).startTimestamp, new Date(start + 600 * minute).toISOString());
  cache.remember("UC_A", "v1", { isLive: true, isUpcoming: false, startTimestamp: null }, start + 2 * minute);
  assert.equal(cache.reuse("UC_A", "v1", start + 3 * minute), undefined);
});

test("entries are scoped by channel, can be forgotten, and the oldest is evicted past the bound", () => {
  const cache = new ScheduleLookupCache({ maxEntries: 2, now: () => start });
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute), start);
  assert.equal(cache.reuse("UC_B", "v1", start + 1), undefined);
  cache.forget("UC_A", "v1");
  assert.equal(cache.size, 0);
  for (const id of ["v1", "v2", "v3"]) cache.remember("UC_A", id, upcoming(start + 600 * minute), start);
  assert.equal(cache.size, 2);
  assert.equal(cache.reuse("UC_A", "v1", start + 1), undefined);
  assert.throws(() => new ScheduleLookupCache({ maxEntries: 0 }), TypeError);
});
