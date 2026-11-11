import assert from "node:assert/strict";
import test from "node:test";

import { createInnertube } from "./fetch-community.mjs";
import { createHelperRuntime, RuntimeState } from "./helper-runtime.mjs";
import { createRealFetchers } from "./real-fetchers.mjs";
import { currentRequestSignal, runWithRequestContext } from "./request-context.mjs";
import { classifyUpstreamError } from "./upstream-errors.mjs";

const unexpectedFetch = async () => {
  throw new Error("unexpected upstream request");
};

function liveCheckClient(sessionUserAgent, contextUserAgent = sessionUserAgent) {
  return { session: { user_agent: sessionUserAgent, context: { client: { userAgent: contextUserAgent } } } };
}

function mismatchedLiveCheckFetchers(fetchImpl = unexpectedFetch, singleAttemptFetchImpl = unexpectedFetch) {
  return createRealFetchers({
    fetchImpl,
    singleAttemptFetchImpl,
    createInnertubeImpl: async () => ({}),
    createLiveCheckInnertubeImpl: async () => liveCheckClient("session-agent", "context-agent"),
  });
}

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

test("prepare creates the collection and live check Innertubes outside a request context", async (t) => {
  const signals = [];
  const liveCheckOptions = [];
  const fetchers = createRealFetchers({
    singleAttemptFetchImpl: unexpectedFetch,
    createInnertubeImpl: async () => {
      signals.push(currentRequestSignal());
      return { getChannel: async () => { throw Object.assign(new Error("stop"), { code: "parser_drift" }); } };
    },
    createLiveCheckInnertubeImpl: async (options) => {
      signals.push(currentRequestSignal());
      liveCheckOptions.push(options);
      return liveCheckClient("test-agent");
    },
  });
  t.after(() => fetchers.close());
  await fetchers.prepare();
  // PO 발급 상태는 첫 라이브 확인 RPC 전부터 helper /health에 실립니다.
  assert.equal(fetchers.proofStatus()?.state, "COLD");
  const controller = new AbortController();
  await assert.rejects(
    () => runWithRequestContext({ requestId: "first-rpc", signal: controller.signal }, () =>
      fetchers.fetchChannel({ channelId: "UC_TEST", kind: "live", maxSuccessResponseBytes: 1 << 20 })),
    (error) => error.code === "parser_drift",
  );
  assert.deepEqual(signals, [undefined, undefined]);
  assert.deepEqual(liveCheckOptions, [{ fetchImpl: unexpectedFetch }]);
});

test("live check user agent mismatch is a helper invariant rather than a transient failure", async () => {
  await assert.rejects(
    () => mismatchedLiveCheckFetchers().prepare(),
    (error) => classifyUpstreamError(error) === "helper_internal_invariant",
  );
});

test("bootstrap faults before READY when the live check Innertube cannot be prepared", async () => {
  const runtime = createHelperRuntime({
    createTransport: async () => ({ fetch: unexpectedFetch, singleAttemptFetch: unexpectedFetch }),
    createFetchers: mismatchedLiveCheckFetchers,
  });
  const result = await runtime.handleBootstrap(JSON.stringify({
    protocol_version: 1,
    limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 2 },
  }));
  assert.equal(result.status, 500);
  assert.equal(result.body.error.code, "helper_internal_invariant");
  assert.equal(runtime.state, RuntimeState.FAULTED);
});
