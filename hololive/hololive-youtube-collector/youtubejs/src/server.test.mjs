import assert from "node:assert/strict";
import test from "node:test";

import {
  createHelperServer,
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
} from "./server.mjs";
import { currentRequestSignal } from "./request-context.mjs";

test("handleCommunityRequest requires channel_id", async () => {
  const result = await handleCommunityRequest(rpcBody({}), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error.message, /channel_id/);
});

test("handleCommunityRequest returns pagination metadata from the injected fetcher", async () => {
  const result = await handleCommunityRequest(
    rpcBody({ channel_id: "UC_TEST", max_results: 3, max_pages: 2 }),
    async ({ channelId, maxResults, maxPages }) => {
      assert.equal(channelId, "UC_TEST");
      assert.equal(maxResults, 3);
      assert.equal(maxPages, 2);
      return {
        posts: [{
          postId: "post-1",
          upstreamPostId: "post-1",
          authorId: "UC_AUTHOR",
          authorName: "Author",
          authorPhoto: [],
          contentText: "hello",
          publishedText: "1 hour ago",
          likeCount: 1,
          commentCount: 0,
          images: [],
        }],
        page_count: 1,
        exhausted: true,
        continuity: "CONTIGUOUS",
        termination_reason: "exhausted",
      };
    },
  );
  assert.equal(result.status, 200);
  assert.equal(result.body.posts[0].postId, "post-1");
  assert.equal(result.body.page_count, 1);
  assert.equal(result.body.exhausted, true);
  assert.equal(result.body.continuity, "CONTIGUOUS");
});

test("handleCommunityRequest fail-closes when the fetcher throws", async () => {
  const result = await handleCommunityRequest(
    rpcBody({ channel_id: "UC_FAIL" }),
    async () => {
      throw new Error("innertube down");
    },
  );
  assert.equal(result.status, 500);
  assert.match(result.body.error.message, /innertube down/);
  assert.equal(result.body.error.code, "helper_internal_invariant");
  assert.equal(result.body.error.class, "INTERNAL");
});

test("handleChannelRequest reports the typed parser response error class", async () => {
  const result = await handleChannelRequest(
    rpcBody({ channel_id: "UC_TEST" }),
    async () => ({ live_sessions: "not-an-array" }),
  );
  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
  assert.equal(result.body.error.class, "DATA_CONTRACT");
});

test("handleCommunityRequest rejects an invalid custom error name", async () => {
  const result = await handleCommunityRequest(
    rpcBody({ channel_id: "UC_TEST" }),
    async () => {
      const error = new Error("custom failure");
      error.name = "invalid custom name";
      throw error;
    },
  );
  assert.equal(result.status, 500);
  assert.equal(result.body.error.class, "INTERNAL");
});

test("handleCommunityRequest rejects invalid JSON", async () => {
  const result = await handleCommunityRequest("{", async () => ({}));
  assert.equal(result.status, 400);
});

test("handleCommunityRequest rejects coercible non-string ids", async () => {
  const result = await handleCommunityRequest(rpcBody({ channel_id: { toString: "UC_FAKE" } }), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error.message, /channel_id/);
});

test("handleContentRequest requires kind", async () => {
  const result = await handleContentRequest(rpcBody({ channel_id: "UC_TEST" }), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error.message, /kind/);
});

test("handleChannelRequest returns the injected channel payload", async () => {
  const result = await handleChannelRequest(
    rpcBody({ channel_id: "UC_TEST" }),
    async ({ channelId }) => ({
      live_sessions: [{ video_id: "vid-1", channel_id: channelId, status: "LIVE" }],
      stats: {},
      profile: {},
      photo: [],
      page_count: 1,
      exhausted: true,
      continuity: "NOT_APPLICABLE",
      termination_reason: "exhausted",
    }),
  );
  assert.equal(result.status, 200);
  assert.equal(result.body.live_sessions[0].video_id, "vid-1");
});

test("handleChannelRequest rejects an unvalidated fetcher response", async () => {
  const result = await handleChannelRequest(
    rpcBody({ channel_id: "UC_TEST" }),
    async () => ({ live_sessions: "not-an-array" }),
  );
  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
});

test("handleChannelRequest rejects fields outside the validated response contract", async () => {
  const result = await handleChannelRequest(
    rpcBody({ channel_id: "UC_TEST" }),
    async () => ({
      live_sessions: [], stats: {}, profile: {}, photo: [],
      page_count: 0, exhausted: true, continuity: "CONTIGUOUS",
      unchecked: "must-not-cross-the-rpc-boundary",
    }),
  );
  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
});

test("handleViewerRequest requires video_id", async () => {
  const result = await handleViewerRequest(rpcBody({}), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error.message, /video_id/);
});

test("health is 503 until bootstrap and collection is helper_not_ready", async () => {
  const http = await import("node:http");
  const server = createHelperServer({
    fetchCommunity() {
      return {
        posts: [],
        page_count: 1,
        exhausted: true,
        continuity: "CONTIGUOUS",
        termination_reason: "exhausted",
      };
    },
  });
  await listenLocal(server);
  try {
    const health = await request(http, server, "GET", "/health");
    assert.equal(health.status, 503);
    assert.equal(health.body.state, "UNCONFIGURED");
    const collection = await request(http, server, "POST", "/v1/community", rpcBody({ channel_id: "UC_TEST" }));
    assert.equal(collection.status, 503);
    assert.equal(collection.body.error.code, "helper_not_ready");
    const boot = await request(http, server, "POST", "/v1/bootstrap", JSON.stringify({
      protocol_version: 1,
      proxy: { enabled: false },
      limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 2 },
    }));
    assert.equal(boot.status, 200);
    assert.equal(boot.body.state, "READY");
    const ready = await request(http, server, "GET", "/health");
    assert.equal(ready.status, 200);
    assert.equal(ready.body.state, "READY");
    assert.equal(ready.body.max_inflight, 2);
  } finally {
    await closeServer(server);
  }
});

test("RPC-009 and RPC-010 reject declared and chunked request overflow once", async () => {
  const http = await import("node:http");
  const server = createHelperServer();
  await listenLocal(server);
  try {
    await request(http, server, "POST", "/v1/bootstrap", JSON.stringify({
      protocol_version: 1,
      proxy: { enabled: false },
      limits: { request_body_bytes: 64, response_body_bytes: 1048576, max_inflight: 2 },
    }));
    const oversized = JSON.stringify({
      protocol_version: 1,
      channel_id: "UC_TEST",
      max_success_response_bytes: 1048576,
      padding: "x".repeat(128),
    });
    const declared = await request(http, server, "POST", "/v1/community", oversized);
    assert.equal(declared.status, 413);
    assert.equal(declared.body.error.code, "request_too_large");
    const chunked = await chunkedRequest(http, server, "/v1/community", oversized);
    assert.equal(chunked.status, 413);
    assert.equal(chunked.body.error.code, "request_too_large");
  } finally {
    await closeServer(server);
  }
});

test("RPC-012 client disconnect aborts only the matching request context", async () => {
  const http = await import("node:http");
  /** @type {(value?: void) => void} */
  let hangStarted = () => {};
  const hangReady = new Promise((resolve) => {
    hangStarted = resolve;
  });
  /** @type {AbortSignal | undefined} */
  let hangSignal;
  /** @type {AbortSignal | undefined} */
  let peerSignal;
  const server = createHelperServer({
    fetchCommunity({ channelId }) {
      const signal = currentRequestSignal();
      if (signal == null) {
        return Promise.reject(new Error("request signal missing"));
      }
      if (channelId === "UC_HANG") {
        return new Promise((_, reject) => {
          hangSignal = signal;
          signal.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          }, { once: true });
          hangStarted();
        });
      }
      return new Promise((resolve, reject) => {
        peerSignal = signal;
        const timer = setTimeout(() => {
          resolve({
            posts: [],
            page_count: 1,
            exhausted: true,
            continuity: "CONTIGUOUS",
            termination_reason: "exhausted",
          });
        }, 40);
        signal.addEventListener("abort", () => {
          clearTimeout(timer);
          reject(new Error("peer request aborted"));
        }, { once: true });
      });
    },
  });
  await listenLocal(server);
  try {
    await request(http, server, "POST", "/v1/bootstrap", JSON.stringify({
      protocol_version: 1,
      proxy: { enabled: false },
      limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 2 },
    }));
    const address = server.address();
    assert.ok(address != null && typeof address !== "string");
    const hangClient = http.request({
      host: "127.0.0.1",
      port: address.port,
      method: "POST",
      path: "/v1/community",
      headers: { "content-type": "application/json" },
    });
    hangClient.on("error", () => {});
    hangClient.write(rpcBody({ channel_id: "UC_HANG" }));
    hangClient.end();
    await hangReady;
    const peer = request(http, server, "POST", "/v1/community", rpcBody({ channel_id: "UC_PEER" }));
    await new Promise((resolve) => setTimeout(resolve, 10));
    hangClient.destroy();
    const peerResult = await peer;
    assert.equal(peerResult.status, 200);
    assert.equal(hangSignal?.aborted, true);
    assert.equal(peerSignal?.aborted, false);
  } finally {
    await closeServer(server);
  }
});

function rpcBody(fields) {
  return JSON.stringify({ protocol_version: 1, max_success_response_bytes: 1048576, ...fields });
}

/** @param {import("node:http").Server} server */
function listenLocal(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(undefined));
  });
}

/** @param {import("node:http").Server} server */
function closeServer(server) {
  return new Promise((resolve, reject) => {
    server.close((err) => err ? reject(err) : resolve(undefined));
  });
}

/**
 * @param {typeof import("node:http")} http
 * @param {import("node:http").Server} server
 * @param {string} method
 * @param {string} path
 * @param {string} [body]
 */
function request(http, server, method, path, body) {
  const address = server.address();
  if (address == null || typeof address === "string") {
    return Promise.reject(new Error("listening address missing"));
  }
  return new Promise((resolve, reject) => {
    const client = http.request({
      host: "127.0.0.1",
      port: address.port,
      method,
      path,
      headers: body ? { "content-type": "application/json", "content-length": Buffer.byteLength(body) } : {},
    }, (res) => {
      const chunks = [];
      res.on("data", (chunk) => chunks.push(chunk));
      res.on("end", () => {
        resolve({ status: res.statusCode ?? 0, body: JSON.parse(Buffer.concat(chunks).toString("utf8")) });
      });
    });
    client.on("error", reject);
    client.end(body ?? undefined);
  });
}

function chunkedRequest(http, server, path, body) {
  const address = server.address();
  if (address == null || typeof address === "string") {
    return Promise.reject(new Error("listening address missing"));
  }
  return new Promise((resolve, reject) => {
    const client = http.request({
      host: "127.0.0.1",
      port: address.port,
      method: "POST",
      path,
      headers: { "content-type": "application/json", "transfer-encoding": "chunked" },
    }, (res) => {
      const chunks = [];
      res.on("data", (chunk) => chunks.push(chunk));
      res.on("end", () => resolve({
        status: res.statusCode ?? 0,
        body: JSON.parse(Buffer.concat(chunks).toString("utf8")),
      }));
    });
    client.on("error", reject);
    client.write(body.slice(0, 32));
    client.end(body.slice(32));
  });
}
