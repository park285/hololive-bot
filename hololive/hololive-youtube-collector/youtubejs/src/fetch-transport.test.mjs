import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";

import { createFetchTransport } from "./fetch-transport.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { rpcErrorResultFor } from "./rpc-validation.mjs";
import { paginate, paginationEnvelopeReserve } from "./pagination.mjs";

test("effective Request semantics reach the origin through local sockets", async () => {
  const fixture = await originFixture();
  const transport = createFetchTransport({ currentSignal: () => undefined });
  try {
    const first = await transport.fetch(`${fixture.originURL}/get`, {
      headers: { "x-case": "url" },
    });
    assert.equal(first.status, 200);
    await first.text();

    const native = new Request(`${fixture.originURL}/native`, { headers: { "x-case": "native" } });
    const nativeResponse = await transport.fetch(native);
    assert.equal(nativeResponse.status, 200);
    await nativeResponse.text();

    const base = new Request(`${fixture.originURL}/override`, {
      method: "POST",
      headers: { "x-base": "ignored" },
      body: "base",
    });
    const overrideResponse = await transport.fetch(base, {
      method: "PUT",
      headers: { "x-case": "override" },
      body: "effective",
      duplex: "half",
    });
    assert.equal(overrideResponse.status, 200);
    await overrideResponse.text();

    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("chunk-a"));
        controller.enqueue(new TextEncoder().encode("-chunk-b"));
        controller.close();
      },
    });
    const streamResponse = await transport.fetch(`${fixture.originURL}/stream`, {
      method: "POST",
      body: stream,
      duplex: "half",
      headers: { "content-type": "application/octet-stream" },
    });
    assert.equal(streamResponse.status, 200);
    await streamResponse.text();

    const redirect = await transport.fetch(`${fixture.originURL}/redirect`, { redirect: "manual" });
    assert.equal(redirect.status, 302);
    await redirect.text();
    assert.deepEqual(
      fixture.requests.map(({ method, path, body }) => ({ method, path, body })),
      [
        { method: "GET", path: "/get", body: "" },
        { method: "GET", path: "/native", body: "" },
        { method: "PUT", path: "/override", body: "effective" },
        { method: "POST", path: "/stream", body: "chunk-a-chunk-b" },
        { method: "GET", path: "/redirect", body: "" },
      ],
    );
    assert.equal(fixture.requests[0].headers["x-case"], "url");
    assert.equal(fixture.requests[1].headers["x-case"], "native");
    assert.equal(fixture.requests[2].headers["x-case"], "override");
  } finally {
    await fixture.close();
  }
});

test("RequestInit abort keeps child-abort provenance while request context stays live", async () => {
  const secret = "secret-init-abort-reason";
  const rpcController = new AbortController();
  const initController = new AbortController();
  initController.abort(new Error(secret));
  const fixture = await originFixture();
  const transport = createFetchTransport({ currentSignal: () => rpcController.signal });
  try {
    const result = await runWithRequestContext(
      { requestId: "live-rpc", signal: rpcController.signal },
      async () => {
        try {
          await transport.fetch(`${fixture.originURL}/init-abort`, { signal: initController.signal });
          throw new Error("fetch succeeded");
        } catch (error) {
          assert.equal(error.code, "helper_internal_invariant");
          assert.doesNotMatch(String(error), new RegExp(secret));
          return rpcErrorResultFor(error);
        }
      },
    );
    assert.equal(result.status, 500);
    assert.equal(result.body.error.code, "helper_internal_invariant");
    assert.equal(JSON.stringify(result.body).includes(secret), false);
    assert.equal(rpcController.signal.aborted, false);
    assert.equal(fixture.requests.length, 0);
  } finally {
    await fixture.close();
  }
});

test("an already-aborted request does not reach the origin", async () => {
  const fixture = await originFixture();
  const controller = new AbortController();
  controller.abort();
  const transport = createFetchTransport({ currentSignal: () => controller.signal });
  try {
    await assert.rejects(
      transport.fetch(`${fixture.originURL}/aborted`),
      (error) => error.code === "collection_canceled",
    );
    assert.equal(fixture.requests.length, 0);
  } finally {
    await fixture.close();
  }
});

test("known transport errors remain explicitly transient", async (t) => {
  const failure = new TypeError("fetch failed", { cause: Object.assign(new Error("reset"), { code: "ECONNRESET" }) });
  t.mock.method(globalThis, "fetch", async () => { throw failure; });
  const transport = createFetchTransport({ currentSignal: () => undefined });
  await assert.rejects(
    transport.fetch("http://origin.test/"),
    (error) => error.code === "collection_failed" && error.failureClass === "TRANSIENT",
  );
});

test("body ECONNRESET remains transient after headers without a second request", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    return new Response(new ReadableStream({
      start(controller) {
        controller.error(Object.assign(new Error("body socket reset"), { code: "ECONNRESET" }));
      },
    }));
  });
  const transport = createFetchTransport({ currentSignal: () => undefined, retryDelayMs: 0 });
  const response = await transport.fetch("https://www.youtube.com/youtubei/v1/browse", { method: "POST" });
  await assert.rejects(response.text(), (error) => {
    const result = rpcErrorResultFor(error);
    assert.equal(result.status, 502);
    assert.equal(result.body.error.code, "collection_failed");
    return true;
  });
  assert.equal(calls, 1);
});

test("real undici body termination uses the same first-page and continuation classification", async (t) => {
  const nativeFetch = globalThis.fetch;
  let calls = 0;
  const origin = createServer((_req, res) => {
    calls++;
    res.writeHead(200, { "content-type": "application/json", "content-length": "100000" });
    res.write('{"items":');
    setTimeout(() => res.destroy(), 20);
  });
  await listen(origin);
  t.after(() => close(origin));
  const url = `http://127.0.0.1:${origin.address().port}/`;
  t.mock.method(globalThis, "fetch", (_request, init) => nativeFetch(url, { signal: init.signal }));
  const transport = createFetchTransport({ currentSignal: () => undefined, retryDelayMs: 0 });

  for (const send of [transport.fetch, transport.singleAttemptFetch]) {
    let bodyFailure;
    const before = calls;
    const response = await send("https://www.youtube.com/youtubei/v1/browse", { method: "POST" });
    await assert.rejects(response.json(), (error) => {
      assert.ok(error instanceof TypeError);
      assert.equal(error.message, "terminated");
      assert.equal(error.cause.code, "UND_ERR_SOCKET");
      bodyFailure = error;
      return true;
    });
    assert.equal(calls, before + 1);
    const result = rpcErrorResultFor(bodyFailure);
    assert.equal(result.status, 502);
    assert.equal(result.body.error.class, "TRANSIENT");

    const partial = await paginate({
      firstPage: { items: [{ id: "first" }], continuation: "next" },
      mapPage: (page) => ({ recognized_shape: true, items: page.items }),
      getContinuation: async () => { throw bodyFailure; },
      maxPages: 2, reservedEnvelopeBytes: paginationEnvelopeReserve({ items: [] }),
    });
    assert.equal(partial.termination_reason, "continuation_transient");
    assert.equal(partial.continuity, "GAP_UNRESOLVED");
    assert.deepEqual(partial.items, [{ id: "first" }]);
  }
});

test("real undici cleanup failures preserve HTTP classification and never schedule another attempt", async (t) => {
  const nativeFetch = globalThis.fetch;
  let calls = 0;
  let status = 500;
  const origin = createServer((_req, res) => {
    calls++;
    res.writeHead(status, { "content-type": "application/json", "content-length": "100000" });
    res.write('{"error":');
    setTimeout(() => res.destroy(), 5);
  });
  await listen(origin);
  t.after(() => close(origin));
  const url = `http://127.0.0.1:${origin.address().port}/`;
  t.mock.method(globalThis, "fetch", async (_request, init) => {
    const response = await nativeFetch(url, { signal: init.signal });
    await new Promise((resolve) => setTimeout(resolve, 40));
    return response;
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined, retryDelayMs: 0,
    observeRetry: () => assert.fail("cleanup failure must stop the attempt"),
  });
  for (const upstreamStatus of [500, 429]) {
    status = upstreamStatus;
    const before = calls;
    await assert.rejects(transport.fetch("https://www.youtube.com/youtubei/v1/browse", { method: "POST" }), (error) => {
      assert.equal(error.cause.cause.code, "UND_ERR_SOCKET");
      const result = rpcErrorResultFor(error);
      assert.equal(result.body.error.code, status === 429 ? "cooldown" : "collection_failed");
      return true;
    });
    assert.equal(calls, before + 1);
  }
});

test("safe Innertube HTTP 500 is retried once with the same request body", async (t) => {
  const events = [];
  const bodies = [];
  let calls = 0;
  let canceled = 0;
  t.mock.method(globalThis, "fetch", async (request) => {
    calls += 1;
    bodies.push(await request.text());
    if (calls === 1) {
      return new Response(new ReadableStream({
        cancel() {
          canceled += 1;
        },
      }), { status: 500 });
    }
    return new Response("ok", { status: 200 });
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: (event) => events.push(event),
  });
  const response = await transport.fetch("https://www.youtube.com/youtubei/v1/browse?prettyPrint=false", {
    method: "POST",
    body: JSON.stringify({ browseId: "UC-test" }),
  });
  assert.equal(await response.text(), "ok");
  assert.equal(calls, 2);
  assert.deepEqual(bodies, [
    JSON.stringify({ browseId: "UC-test" }),
    JSON.stringify({ browseId: "UC-test" }),
  ]);
  assert.equal(canceled, 1);
  assert.deepEqual(events, [{
    endpoint: "browse",
    reason: "http_status",
    statusCode: 500,
    delayMs: 0,
    attempt: 2,
    maxAttempts: 2,
  }]);
});

test("safe Innertube transient network failure is retried once", async (t) => {
  const events = [];
  let calls = 0;
  const failure = new TypeError("fetch failed", {
    cause: Object.assign(new Error("reset"), { code: "ECONNRESET" }),
  });
  t.mock.method(globalThis, "fetch", async () => {
    calls += 1;
    if (calls === 1) throw failure;
    return new Response("ok");
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: (event) => events.push(event),
  });
  const response = await transport.fetch("https://www.youtube.com/youtubei/v1/player", {
    method: "POST",
    body: "{}",
  });
  assert.equal(await response.text(), "ok");
  assert.equal(calls, 2);
  assert.deepEqual(events, [{
    endpoint: "player",
    reason: "network",
    delayMs: 0,
    attempt: 2,
    maxAttempts: 2,
  }]);
});

test("unsafe Innertube endpoint is never retried", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls += 1;
    return new Response("failed", { status: 500 });
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: () => assert.fail("unsafe endpoint scheduled a retry"),
  });
  await assert.rejects(
    transport.fetch("https://www.youtube.com/youtubei/v1/log_event", {
      method: "POST",
      body: "{}",
    }),
    (error) => error.code === "collection_failed",
  );
  assert.equal(calls, 1);
});

test("upstream 429 reaches RPC cooldown without retry or response-body disclosure", async (t) => {
  let calls = 0;
  let canceled = 0;
  const events = [];
  t.mock.method(globalThis, "fetch", async () => {
    calls += 1;
    return new Response(new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("private upstream response"));
      },
      cancel() { canceled += 1; },
    }), { status: 429 });
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: (event) => events.push(event),
  });
  await assert.rejects(
    transport.fetch("https://www.youtube.com/youtubei/v1/browse", {
      method: "POST", body: "{}",
    }),
    (error) => {
      const result = rpcErrorResultFor(error);
      assert.equal(result.status, 429);
      assert.equal(result.body.error.code, "cooldown");
      assert.equal(result.body.error.class, "COOLDOWN");
      assert.deepEqual(result.body.error.retry, { kind: "default" });
      assert.equal(JSON.stringify(result).includes("private upstream response"), false);
      return true;
    },
  );
  assert.equal(calls, 1);
  assert.equal(canceled, 1);
  assert.deepEqual(events, []);
});

test("non-transient 5xx statuses are never retried", async (t) => {
  const statuses = [501, 502, 504];
  const events = [];
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    const status = statuses[calls];
    calls += 1;
    return new Response("failed", { status });
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: (event) => events.push(event),
  });
  for (const status of statuses) {
    const before = calls;
    await assert.rejects(
      transport.fetch("https://www.youtube.com/youtubei/v1/browse", {
        method: "POST",
        body: "{}",
      }),
      (error) => error.code === "collection_failed",
    );
    assert.equal(calls, before + 1, `HTTP ${status} must not retry`);
  }
  assert.equal(calls, statuses.length);
  assert.deepEqual(events, []);
});

test("retry exhaustion preserves the typed transient failure", async (t) => {
  let canceled = 0;
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls += 1;
    return new Response(new ReadableStream({
      cancel() {
        canceled += 1;
      },
    }), { status: 500 });
  });
  const transport = createFetchTransport({
    currentSignal: () => undefined,
    retryDelayMs: 0,
    observeRetry: () => {},
  });
  let failure;
  try {
    await transport.fetch("https://www.youtube.com/youtubei/v1/browse", { method: "POST" });
  } catch (error) {
    failure = error;
  }
  assert.equal(failure?.code, "collection_failed");
  assert.equal(failure?.failureClass, "TRANSIENT");
  assert.equal(calls, 2);
  assert.equal(canceled, 2);
  const result = rpcErrorResultFor(failure);
  assert.equal(result.status, 502);
  assert.equal(result.body.error.code, "collection_failed");
  assert.equal(result.body.error.class, "TRANSIENT");
});

test("request cancellation stops a scheduled retry before the second attempt", async (t) => {
  const controller = new AbortController();
  let scheduled;
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls += 1;
    return new Response("failed", { status: 500 });
  });
  const transport = createFetchTransport({
    currentSignal: () => controller.signal,
    observeRetry: (event) => {
      scheduled = event;
      controller.abort();
    },
  });
  await assert.rejects(
    transport.fetch("https://www.youtube.com/youtubei/v1/next", {
      method: "POST",
      body: "{}",
    }),
    (error) => error.code === "collection_canceled",
  );
  assert.equal(calls, 1);
  assert.equal(scheduled.endpoint, "next");
  assert.equal(scheduled.attempt, 2);
  assert.ok(scheduled.delayMs >= 100 && scheduled.delayMs <= 300);
});

test("request-scoped cancellation affects only one of twenty requests", async () => {
  const fixture = await originFixture();
  let signal;
  const transport = createFetchTransport({ currentSignal: () => signal });
  try {
    const requests = Array.from({ length: 20 }, (_, index) => {
      const controller = new AbortController();
      signal = controller.signal;
      const pending = transport.fetch(`${fixture.originURL}/concurrent/${index}`).then((response) => response.text());
      if (index === 7) {
        controller.abort();
      }
      return pending;
    });
    signal = undefined;
    const results = await Promise.allSettled(requests);
    assert.equal(results.filter((result) => result.status === "rejected").length, 1);
    assert.equal(results.filter((result) => result.status === "fulfilled").length, 19);
  } finally {
    await fixture.close();
  }
});

test("locked request bodies fail as helper_internal_invariant", async () => {
  const transport = createFetchTransport({ currentSignal: () => undefined });
  const request = new Request("http://127.0.0.1/locked", {
    method: "POST",
    body: "body",
  });
  const reader = request.body.getReader();
  try {
    await assert.rejects(
      transport.fetch(request),
      (error) => error.code === "helper_internal_invariant",
    );
  } finally {
    reader.releaseLock();
  }
});

async function originFixture() {
  const requests = [];
  const origin = createServer((req, res) => {
    const chunks = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => {
      requests.push({
        method: req.method,
        path: req.url,
        headers: req.headers,
        body: Buffer.concat(chunks).toString("utf8"),
      });
      if (req.url === "/redirect") {
        res.writeHead(302, { location: "/destination" });
      } else {
        res.writeHead(200, { "content-type": "text/plain" });
      }
      res.end("ok");
    });
  });
  await listen(origin);
  const originAddress = origin.address();
  if (originAddress == null || typeof originAddress === "string") {
    throw new Error("fixture address missing");
  }
  return {
    requests,
    originURL: `http://127.0.0.1:${originAddress.port}`,
    async close() {
      await close(origin);
    },
  };
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });
}

function close(server) {
  return new Promise((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
    server.closeAllConnections?.();
  });
}
