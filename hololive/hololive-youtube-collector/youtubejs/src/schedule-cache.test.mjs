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

test("near-start, live, unscheduled, past, and restricted results are never reused", () => {
  const cache = new ScheduleLookupCache({ now: () => start });
  const results = {
    near: upcoming(start + 30 * minute),
    past: upcoming(start - minute),
    live: { isLive: true, isUpcoming: false, startTimestamp: new Date(start).toISOString() },
    "not-upcoming": { isLive: false, isUpcoming: false, startTimestamp: new Date(start + 600 * minute).toISOString() },
    "no-time": { isLive: false, isUpcoming: true, startTimestamp: null },
    members: { scheduleUnavailableReason: "access_restricted" },
  };
  for (const [id, metadata] of Object.entries(results)) {
    cache.remember("UC_A", id, metadata, start);
    assert.equal(cache.reuse("UC_A", id, start + 1), undefined, id);
  }
});

test("an invalidated or in-progress slot blocks a slower lookup from an earlier slot", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  // 다음 슬롯이 목록에서 시각을 직접 보거나 LIVE·임박 판정을 받은 뒤, 앞 슬롯의 느린 조회가 끝납니다.
  cache.remember("UC_A", "listed", upcoming(start + 600 * minute), start);
  cache.forget("UC_A", "listed", start + 2 * minute);
  cache.remember("UC_A", "listed", upcoming(start + 600 * minute), start + minute);
  cache.remember("UC_A", "went-live", { isLive: true, isUpcoming: false, startTimestamp: null }, start + 2 * minute);
  cache.remember("UC_A", "went-live", upcoming(start + 600 * minute), start + minute);
  // 다음 슬롯의 조회가 아직 진행 중이어도 앞 슬롯의 결과는 저장하지 않습니다.
  cache.beginLookup("UC_A", "pending", start + 2 * minute);
  cache.remember("UC_A", "pending", upcoming(start + 600 * minute), start + minute);
  for (const id of ["listed", "went-live", "pending"]) {
    assert.equal(cache.reuse("UC_A", id, start + 4 * minute), undefined, id);
  }
  // 그 슬롯의 조회가 끝나면 정상으로 기록합니다.
  cache.remember("UC_A", "pending", upcoming(start + 600 * minute), start + 2 * minute);
  assert.equal(cache.reuse("UC_A", "pending", start + 4 * minute).observedAt, new Date(start + 2 * minute).toISOString());
});

test("a failed recheck leaves its marker so later slots look up again", () => {
  const cache = new ScheduleLookupCache({ now: () => start });
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute), start);
  // 같은 슬롯의 재시도가 조회를 시작한 뒤 실패해 결과를 기록하지 못합니다.
  cache.beginLookup("UC_A", "v1", start);
  assert.equal(cache.reuse("UC_A", "v1", start + 2 * minute), undefined);
});

test("forgetting an absent entry stores nothing, and markers expire with the reuse bound", () => {
  const time = clock();
  const cache = new ScheduleLookupCache({ now: time.now });
  cache.forget("UC_A", "listed-only", start);
  assert.equal(cache.size, 0);
  cache.beginLookup("UC_A", "v1", start);
  time.advance(10 * minute);
  cache.remember("UC_A", "v1", upcoming(start + 600 * minute), start - minute);
  assert.notEqual(cache.reuse("UC_A", "v1", start), undefined);
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
  cache.forget("UC_A", "v1", start);
  assert.equal(cache.reuse("UC_A", "v1", start + 1), undefined);
  for (const id of ["v1", "v2", "v3"]) cache.remember("UC_A", id, upcoming(start + 600 * minute), start);
  assert.equal(cache.size, 2);
  assert.equal(cache.reuse("UC_A", "v1", start + 1), undefined);
  assert.throws(() => new ScheduleLookupCache({ maxEntries: 0 }), TypeError);
});
