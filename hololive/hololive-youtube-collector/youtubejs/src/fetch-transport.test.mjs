import assert from "node:assert/strict";
import { createServer, request as httpRequest } from "node:http";
import { connect } from "node:net";
import test from "node:test";

import { createFetchTransport } from "./fetch-transport.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { rpcErrorResultFor } from "./rpc-validation.mjs";

test("PXY-001 PXY-002 PXY-003 PXY-004 PXY-005 preserve effective Request semantics through local sockets", async () => {
  const fixture = await proxyFixture();
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: fixture.proxyURL },
    currentSignal: () => undefined,
  });
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
    assert.equal(fixture.proxyRequests, 5);
  } finally {
    await transport.close();
    try {
      await new Promise((resolve) => setTimeout(resolve, 10));
      assert.equal(fixture.activeProxyConnections, 0);
    } finally {
      await fixture.close();
    }
  }
});

test("PXY RequestInit abort keeps child-abort provenance while request context stays live", async () => {
  const secret = "secret-init-abort-reason";
  const rpcController = new AbortController();
  const initController = new AbortController();
  initController.abort(new Error(secret));
  const fixture = await proxyFixture();
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: fixture.proxyURL },
    currentSignal: () => rpcController.signal,
  });
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
    await transport.close();
    await fixture.close();
  }
});

test("PXY-006 an already-aborted request does not reach the origin", async () => {
  const fixture = await proxyFixture();
  const controller = new AbortController();
  controller.abort();
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: fixture.proxyURL },
    currentSignal: () => controller.signal,
  });
  try {
    await assert.rejects(
      transport.fetch(`${fixture.originURL}/aborted`),
      (error) => error.code === "collection_canceled",
    );
    assert.equal(fixture.requests.length, 0);
  } finally {
    await transport.close();
    await fixture.close();
  }
});

test("known transport errors remain explicitly transient", async () => {
  class ProxyAgent {
    async close() {}
    destroy() {}
  }
  const failure = new TypeError("fetch failed", { cause: Object.assign(new Error("reset"), { code: "ECONNRESET" }) });
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: "http://proxy.test:8080" },
    currentSignal: () => undefined,
    loadUndici: async () => ({
      ProxyAgent,
      fetch: async () => { throw failure; },
    }),
  });
  try {
    await assert.rejects(
      transport.fetch("http://origin.test/"),
      (error) => error.code === "collection_failed" && error.failureClass === "TRANSIENT",
    );
  } finally {
    await transport.close();
  }
});

test("PXY-007 request-scoped cancellation affects only one of twenty requests", async () => {
  const fixture = await proxyFixture();
  let signal;
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: fixture.proxyURL },
    currentSignal: () => signal,
  });
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
    await transport.close();
    await fixture.close();
  }
});

test("PXY-008 PXY-009 bootstrap constructs one agent and close is idempotent", async () => {
  let constructed = 0;
  let closed = 0;
  class ProxyAgent {
    constructor() {
      constructed += 1;
    }
    async close() {
      closed += 1;
    }
    destroy() {}
  }
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: "http://proxy.test:8080" },
    currentSignal: () => undefined,
    loadUndici: async () => ({ ProxyAgent, fetch: async () => new Response() }),
  });
  assert.equal(constructed, 1);
  await Promise.all([transport.close(), transport.close()]);
  assert.equal(closed, 1);
});

test("PXY-009 close rejection destroys exactly once and PXY-010 redacts credentials", async () => {
  let destroyed = 0;
  class ProxyAgent {
    async close() {
      throw new Error("close failed");
    }
    destroy() {
      destroyed += 1;
    }
  }
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: "http://user:password@proxy.test:8080" },
    currentSignal: () => undefined,
    loadUndici: async () => ({ ProxyAgent, fetch: async () => new Response() }),
  });
  await assert.rejects(transport.close(), /close failed/);
  await assert.rejects(transport.close(), /close failed/);
  assert.equal(destroyed, 1);

  const secret = "super-secret-password";
  await assert.rejects(
    createFetchTransport({
      proxy: { enabled: true, url: `http://user:${secret}@proxy.test:8080/path` },
      currentSignal: () => undefined,
    }),
    (error) => {
      assert.doesNotMatch(String(error), new RegExp(secret));
      return true;
    },
  );
});

test("locked request bodies fail as helper_internal_invariant", async () => {
  const transport = await createFetchTransport({
    proxy: { enabled: false },
    currentSignal: () => undefined,
  });
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
    await transport.close();
  }
});

test("PXY-009 close timeout destroys exactly once", async () => {
  let destroyed = 0;
  class ProxyAgent {
    close() {
      return new Promise(() => {});
    }
    destroy() {
      destroyed += 1;
    }
  }
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: "http://proxy.test:8080" },
    currentSignal: () => undefined,
    closeTimeoutMs: 5,
    loadUndici: async () => ({ ProxyAgent, fetch: async () => new Response() }),
  });
  await assert.rejects(transport.close(), /timed out/);
  await assert.rejects(transport.close(), /timed out/);
  assert.equal(destroyed, 1);
});

async function proxyFixture() {
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
  let connects = 0;
  let proxyRequests = 0;
  const tunnels = new Set();
  const proxySockets = new Set();
  const proxy = createServer((req, res) => {
    proxyRequests += 1;
    const upstream = httpRequest(req.url, {
      method: req.method,
      headers: req.headers,
    }, (upstreamResponse) => {
      res.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
      upstreamResponse.pipe(res);
    });
    upstream.on("error", (error) => res.destroy(error));
    req.pipe(upstream);
  });
  proxy.on("connection", (socket) => {
    proxySockets.add(socket);
    socket.on("close", () => proxySockets.delete(socket));
  });
  proxy.on("connect", (req, client, firstPacket) => {
    connects += 1;
    const [host, port] = String(req.url).split(":");
    const upstream = connect(Number(port), host, () => {
      tunnels.add(client);
      tunnels.add(upstream);
      client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (firstPacket.length > 0) {
        upstream.write(firstPacket);
      }
      client.pipe(upstream);
      upstream.pipe(client);
    });
    client.on("close", () => tunnels.delete(client));
    upstream.on("close", () => tunnels.delete(upstream));
    upstream.on("error", () => client.destroy());
  });
  await listen(proxy);
  const originAddress = origin.address();
  const proxyAddress = proxy.address();
  if (originAddress == null || typeof originAddress === "string" || proxyAddress == null || typeof proxyAddress === "string") {
    throw new Error("fixture address missing");
  }
  return {
    requests,
    get connects() {
      return connects;
    },
    get proxyRequests() {
      return proxyRequests;
    },
    get activeProxyConnections() {
      return proxySockets.size;
    },
    originURL: `http://127.0.0.1:${originAddress.port}`,
    proxyURL: `http://127.0.0.1:${proxyAddress.port}`,
    async close() {
      for (const tunnel of tunnels) {
        tunnel.destroy();
      }
      await Promise.all([close(origin), close(proxy)]);
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
