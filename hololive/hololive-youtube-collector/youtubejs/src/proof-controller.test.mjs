import assert from "node:assert/strict";
import test from "node:test";
import { setImmediate as tick } from "node:timers/promises";
import { ProofController } from "./proof-controller.mjs";
import { ProofError } from "./proof-broker.mjs";
import { currentRequestSignal, runWithRequestContext } from "./request-context.mjs";

function fixture(t, options = {}) {
  let now = 1_000;
  let wall = Date.parse("2026-09-27T00:00:00Z");
  let sequence = 0;
  const sent = [];
  const requests = [];
  const retired = [];
  const broker = {
    async freshGeneration() { return `generation-${++sequence}`; },
    async prepare() {},
    async challenge() { return "snapshot"; },
    async activate() {},
    async mint() { return "valid-proof"; },
    async retire(id) { retired.push(id); },
    close() {},
    ...options.broker,
  };
  const controller = new ProofController({
    broker, userAgent: "supported-client", clock: () => now, wallClock: () => wall,
    async fetchImpl(url, init) {
      requests.push(url);
      await options.beforeFetch?.(url, init);
      // 실제 transport처럼 상속된 collection 취소도 적용합니다.
      currentRequestSignal()?.throwIfAborted();
      init.signal.throwIfAborted();
      if (url.endsWith("/Create")) {
        const challenge = options.challenge ?? ["message", ["interpreter"], [], "hash", "program", "global"];
        return Response.json([challenge]);
      }
      return Response.json(options.integrity ?? ["aW50ZWdyaXR5", 3_600, 100]);
    },
  });
  t.after(() => controller.close());
  const send = async (token) => { sent.push(token !== undefined); };
  return {
    controller, broker, sent, requests, retired,
    player: (signal) => controller.runPlayer("video-id", send, signal),
    advance(ms) { now += ms; },
    shiftWall(ms) { wall += ms; },
  };
}

async function warm(f) {
  await f.player();
  await tick();
  assert.equal(f.controller.status().state, "READY");
}

function deadlineClock(t) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  t.mock.method(AbortSignal, "timeout", (ms) => {
    const controller = new AbortController();
    setTimeout(() => controller.abort(new DOMException("timed out", "TimeoutError")), ms);
    return controller.signal;
  });
}

test("replacement SDK startup does not consume the issuance deadline", async (t) => {
  deadlineClock(t);
  const ready = Promise.withResolvers();
  const waiting = Promise.withResolvers();
  let generations = 0;
  const f = fixture(t, {
    broker: {
      async freshGeneration(signal) {
        if (++generations === 1) return "generation-1";
        waiting.resolve();
        signal.addEventListener("abort", () => ready.reject(signal.reason), { once: true });
        await ready.promise;
        signal.throwIfAborted();
        return "generation-2";
      },
    },
  });
  await warm(f);
  f.advance(3_270_000);
  await f.player();
  await waiting.promise;
  t.mock.timers.tick(16_000);
  await tick();
  ready.resolve();
  await f.controller.attempt;
  assert.equal(f.controller.status().state, "READY");
  assert.equal(f.controller.status().generation, "generation-2");
  await f.player();
  assert.deepEqual(f.sent, [false, false, true]);
  assert.equal(f.requests.length, 4);
});

test("issuance still expires at fifteen seconds after worker readiness", async (t) => {
  deadlineClock(t);
  const release = Promise.withResolvers();
  const fetching = Promise.withResolvers();
  const f = fixture(t, { beforeFetch: async () => { fetching.resolve(); await release.promise; } });
  await f.player();
  await fetching.promise;
  t.mock.timers.tick(15_000);
  release.resolve();
  await f.controller.attempt;
  assert.equal(f.controller.status().state, "UNAVAILABLE");
  assert.deepEqual(f.retired, ["generation-1"]);
  assert.equal(f.requests.length, 1);
  await f.player();
  assert.deepEqual(f.sent, [false, false]);
});

test("upstream Create waits for confirmed preparation", async (t) => {
  const prepared = Promise.withResolvers();
  const preparing = Promise.withResolvers();
  let completed = false;
  const f = fixture(t, {
    broker: {
      async prepare(generation, userAgent) {
        assert.equal(generation, "generation-1");
        assert.equal(userAgent, "supported-client");
        preparing.resolve();
        await prepared.promise;
        completed = true;
      },
      async challenge() {
        assert.equal(completed, true);
        return "snapshot";
      },
    },
    beforeFetch() { assert.equal(completed, true); },
  });
  await f.player();
  await preparing.promise;
  assert.deepEqual(f.requests, []);
  prepared.resolve();
  await tick();
  assert.equal(f.controller.status().state, "READY");
  assert.equal(f.requests.length, 2);
});

test("failed preparation retires its generation without upstream requests", async (t) => {
  const f = fixture(t, {
    broker: {
      async prepare() { throw new ProofError("broker_worker_failed"); },
      async challenge() { assert.fail("challenge must not follow failed preparation"); },
    },
  });
  await f.player();
  await tick();
  assert.deepEqual(f.requests, []);
  assert.deepEqual(f.retired, ["generation-1"]);
  assert.equal(f.controller.status().last_error, "broker_worker_failed");
});

test("cleanup failure preserves the issuance cause until a new generation succeeds", async (t) => {
  let fail = true;
  const f = fixture(t, {
    broker: {
      async prepare() { if (fail) throw new ProofError("broker_worker_timeout"); },
      async retire() { if (fail) throw new ProofError("broker_worker_failed"); },
    },
  });
  await f.player();
  await tick();
  assert.equal(f.controller.status().last_error, "broker_worker_timeout");
  assert.equal(f.controller.status().cleanup_error, "broker_worker_failed");
  assert.equal(f.controller.status().state, "UNAVAILABLE");
  assert.deepEqual(f.requests, []);
  fail = false;
  f.advance(300_000);
  await warm(f);
  await f.player();
  assert.equal(f.controller.status().last_error, undefined);
  assert.equal(f.controller.status().cleanup_error, undefined);
  assert.deepEqual(f.sent, [false, false, true]);
});

test("canceled preparation retires its generation without upstream requests", async (t) => {
  const preparing = Promise.withResolvers();
  const f = fixture(t, {
    broker: {
      async prepare(_generation, _userAgent, signal) {
        preparing.resolve();
        await new Promise((_resolve, reject) => {
          signal.addEventListener("abort", () => reject(signal.reason), { once: true });
        });
      },
      async challenge() { assert.fail("challenge must not follow canceled preparation"); },
    },
  });
  await f.player();
  await preparing.promise;
  assert.deepEqual(f.requests, []);
  await f.controller.close();
  assert.deepEqual(f.requests, []);
  assert.deepEqual(f.retired, ["generation-1"]);
  assert.equal(f.controller.status().state, "STOPPED");
});

test("concurrent cold players share issuance that survives the initiating RPC cancellation", async (t) => {
  const gate = Promise.withResolvers();
  const f = fixture(t, { beforeFetch: () => gate.promise });
  const rpc = new AbortController();
  await runWithRequestContext({ requestId: "canceled-rpc", signal: rpc.signal }, () => f.player(rpc.signal));
  await f.player();
  rpc.abort();
  gate.resolve();
  await tick();
  await f.player();
  assert.deepEqual(f.sent, [false, false, true]);
  assert.equal(f.controller.status().bootstrap_attempts, 1);
  assert.equal(f.requests.length, 2);
  assert.equal(f.controller.status().state, "READY");
});

test("provider fallback data is never minted and failed issuance respects the start interval", async (t) => {
  let minted = 0;
  const f = fixture(t, {
    integrity: [null, 3_600, 100, "must-not-be-used"],
    broker: { async mint() { minted += 1; throw new Error("unexpected mint"); } },
  });
  await f.player();
  await tick();
  assert.equal(f.controller.status().state, "UNAVAILABLE");
  assert.equal(f.controller.status().last_error, "integrity_structure");
  f.advance(299_999);
  await f.player();
  assert.equal(f.requests.length, 2);
  assert.equal(minted, 0);
  assert.deepEqual(f.sent, [false, false]);
  assert.deepEqual(f.retired, ["generation-1"]);
  f.advance(1);
  await f.player();
  await tick();
  assert.equal(f.controller.status().bootstrap_attempts, 2);
  assert.equal(f.requests.length, 4);
});

test("wall-clock changes do not expire a valid minter and refresh replaces its generation", async (t) => {
  const f = fixture(t);
  await warm(f);
  const first = f.controller.status().generation;
  f.shiftWall(86_400_000);
  await f.player();
  assert.equal(f.controller.status().generation, first);
  assert.deepEqual(f.sent, [false, true]);
  f.advance(3_270_000);
  await f.player();
  await tick();
  await f.player();
  assert.notEqual(f.controller.status().generation, first);
  assert.equal(f.controller.status().bootstrap_successes, 2);
  assert.equal(f.requests.length, 4);
  assert.deepEqual(f.sent, [false, true, false, true]);
});

test("a token completing after its monotonic deadline is not attached", async (t) => {
  const minted = Promise.withResolvers();
  const f = fixture(t, { broker: { mint: () => minted.promise } });
  await warm(f);
  const player = f.player();
  f.advance(3_570_000);
  minted.resolve("late-proof");
  await player;
  assert.deepEqual(f.sent, [false, false]);
  assert.equal(f.controller.status().state, "EXPIRED");
  assert.equal(f.controller.status().attached_total, 0);
});

test("a late old-generation mint cannot cross a successful refresh", async (t) => {
  const oldMint = Promise.withResolvers();
  let delayOld = true;
  const f = fixture(t, { broker: { async mint() { return delayOld ? oldMint.promise : "current-proof"; } } });
  await warm(f);
  const oldPlayer = f.player();
  f.advance(3_270_000);
  await f.player();
  await tick();
  const replacement = f.controller.status().generation;
  oldMint.resolve("old-proof");
  await oldPlayer;
  delayOld = false;
  await f.player();
  assert.equal(f.controller.status().generation, replacement);
  assert.equal(f.controller.status().state, "READY");
  assert.deepEqual(f.sent, [false, false, false, true]);
  assert.equal(f.controller.status().attached_total, 1);
});

test("unexpected worker failure leaves exactly one un-tokened player and no immediate issuance replay", async (t) => {
  const f = fixture(t, { broker: { async mint() { throw new ProofError("broker_worker_failed"); } } });
  await warm(f);
  await f.player();
  await f.player();
  assert.equal(f.controller.status().state, "UNAVAILABLE");
  assert.deepEqual(f.sent, [false, false, false]);
  assert.equal(f.requests.length, 2);
  assert.equal(f.controller.status().attached_total, 0);
});

test("untrusted interpreter URLs never expand the network allowlist", async (t) => {
  const f = fixture(t, { challenge: ["message", [], ["https://example.invalid/steal"], "hash", "program", "global"] });
  await f.player();
  await tick();
  assert.equal(f.controller.status().last_error, "interpreter_url");
  assert.deepEqual(f.requests, ["https://jnn-pa.googleapis.com/$rpc/google.internal.waa.v1.Waa/Create"]);
  assert.deepEqual(f.retired, ["generation-1"]);
});

test("close aborts pending issuance and cannot create new player or issuer work", async (t) => {
  const started = Promise.withResolvers();
  const f = fixture(t, { beforeFetch: (_url, init) => new Promise((_resolve, reject) => {
    started.resolve();
    init.signal.addEventListener("abort", () => reject(init.signal.reason), { once: true });
  }) });
  await f.player();
  await started.promise;
  await f.controller.close();
  assert.equal(f.controller.status().state, "STOPPED");
  await assert.rejects(() => f.player(), { name: "AbortError" });
  assert.equal(f.requests.length, 1);
  assert.deepEqual(f.sent, [false]);
});

test("a canceled collection mint does not discard another caller's valid generation", async (t) => {
  const started = Promise.withResolvers();
  let calls = 0;
  const f = fixture(t, { broker: { async mint(_generation, _video, signal) {
    if (++calls > 1) return "current-proof";
    started.resolve();
    return new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(signal.reason), { once: true }));
  } } });
  await warm(f);
  const generation = f.controller.status().generation;
  const rpc = new AbortController();
  const canceled = assert.rejects(f.player(rpc.signal), { name: "AbortError" });
  await started.promise;
  rpc.abort();
  await canceled;
  await f.player();
  assert.equal(f.controller.status().generation, generation);
  assert.equal(f.controller.status().state, "READY");
  assert.deepEqual(f.sent, [false, true]);
  assert.equal(f.requests.length, 2);
});
