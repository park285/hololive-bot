import assert from "node:assert/strict";
import test from "node:test";

import { createInnertube } from "./fetch-community.mjs";
import { createRealFetchers } from "./real-fetchers.mjs";
import { currentRequestSignal, runWithRequestContext } from "./request-context.mjs";

test("rejected Innertube init clears the promise", async () => {
  let calls = 0;
  const fetchers = createRealFetchers({
    createInnertubeImpl: async () => {
      calls += 1;
      throw new Error("init failed");
    },
  });
  await assert.rejects(() => fetchers.fetchChannel({ channelId: "UC_TEST", kind: "live", maxSuccessResponseBytes: 1 << 20 }), /init failed/);
  await assert.rejects(() => fetchers.fetchChannel({ channelId: "UC_TEST", kind: "live", maxSuccessResponseBytes: 1 << 20 }), /init failed/);
  assert.equal(calls, 2);
});

test("concurrent Innertube init shares one promise", async () => {
  let calls = 0;
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const fetchers = createRealFetchers({
    createInnertubeImpl: async () => {
      calls += 1;
      await gate;
      return { getChannel() {} };
    },
  });
  const first = fetchers.fetchChannel({ channelId: "UC_TEST", kind: "live", maxSuccessResponseBytes: 1 << 20 });
  const second = fetchers.fetchChannel({ channelId: "UC_OTHER", kind: "live", maxSuccessResponseBytes: 1 << 20 });
  release();
  await Promise.allSettled([first, second]);
  assert.equal(calls, 1);
});

test("collection Innertube is created without an upstream config request", async () => {
  // config 요청 실패를 SDK가 삼키면 이후 수집이 config 없는 세션에 고정되므로 생성 중 upstream 호출이 없어야 합니다.
  const requests = [];
  const innertube = await createInnertube({
    fetchImpl: async (input) => {
      requests.push(String(input instanceof Request ? input.url : input));
      throw new Error("unexpected upstream request");
    },
  });
  assert.equal(typeof innertube.getChannel, "function");
  assert.deepEqual(requests, []);
});

test("prepare creates the shared collection Innertube outside a request context", async () => {
  const signals = [];
  const fetchers = createRealFetchers({
    createInnertubeImpl: async () => {
      signals.push(currentRequestSignal());
      return { getChannel: async () => { throw Object.assign(new Error("stop"), { code: "parser_drift" }); } };
    },
  });
  await fetchers.prepare();
  const controller = new AbortController();
  await assert.rejects(
    () => runWithRequestContext({ requestId: "first-rpc", signal: controller.signal }, () =>
      fetchers.fetchChannel({ channelId: "UC_TEST", kind: "live", maxSuccessResponseBytes: 1 << 20 })),
    (error) => error.code === "parser_drift",
  );
  assert.deepEqual(signals, [undefined]);
});
