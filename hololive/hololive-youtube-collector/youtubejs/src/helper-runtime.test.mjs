import assert from "node:assert/strict";
import test from "node:test";

import { createHelperRuntime, parseBootstrapRequest, RuntimeState } from "./helper-runtime.mjs";
import { stubFetchers } from "./real-fetchers.mjs";

const validBootstrap = {
  protocol_version: 1,
  limits: {
    request_body_bytes: 65536,
    response_body_bytes: 1048576,
    max_inflight: 2,
  },
};

function bootstrapBody(overrides = {}) {
  return JSON.stringify({ ...validBootstrap, ...overrides });
}

test("first bootstrap becomes READY and echoes limits", async () => {
  let created = 0;
  const runtime = createHelperRuntime({
    createTransport: async () => {
      created += 1;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => stubFetchers,
  });
  const result = await runtime.handleBootstrap(bootstrapBody());
  assert.equal(result.status, 200);
  assert.deepEqual(result.body, {
    protocol_version: 1,
    state: RuntimeState.READY,
    request_body_bytes: 65536,
    response_body_bytes: 1048576,
    max_inflight: 2,
  });
  assert.equal(created, 1);
  assert.equal(runtime.healthStatus(), 200);
});

test("equal bootstrap replay is idempotent and keeps one transport", async () => {
  let created = 0;
  const runtime = createHelperRuntime({
    createTransport: async () => {
      created += 1;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => stubFetchers,
  });
  await runtime.handleBootstrap(bootstrapBody());
  const replay = await runtime.handleBootstrap(bootstrapBody());
  assert.equal(replay.status, 200);
  assert.equal(replay.body.state, RuntimeState.READY);
  assert.equal(created, 1);
});

test("concurrent equal bootstrap shares one transport", async () => {
  let created = 0;
  let release;
  const blocked = new Promise((resolve) => {
    release = resolve;
  });
  const runtime = createHelperRuntime({
    createTransport: async () => {
      created += 1;
      await blocked;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => stubFetchers,
  });
  const first = runtime.handleBootstrap(bootstrapBody());
  const second = runtime.handleBootstrap(bootstrapBody());
  assert.equal(created, 1);
  release();
  const results = await Promise.all([first, second]);
  assert.deepEqual(results, [results[0], results[0]]);
  assert.equal(results[0].status, 200);
  assert.equal(created, 1);
});

test("concurrent conflicting bootstrap cannot replace pending config", async () => {
  let release;
  const blocked = new Promise((resolve) => {
    release = resolve;
  });
  const runtime = createHelperRuntime({
    createTransport: async () => {
      await blocked;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => stubFetchers,
  });
  const first = runtime.handleBootstrap(bootstrapBody());
  const conflict = await runtime.handleBootstrap(bootstrapBody({
    limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 3 },
  }));
  assert.equal(conflict.status, 409);
  release();
  assert.equal((await first).status, 200);
  assert.equal(runtime.maxInflight, 2);
});

test("conflicting bootstrap replay keeps the original config", async () => {
  const runtime = createHelperRuntime({
    createTransport: async () => ({ fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch }),
    createFetchers: () => stubFetchers,
  });
  await runtime.handleBootstrap(bootstrapBody());
  const conflict = await runtime.handleBootstrap(bootstrapBody({
    limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 3 },
  }));
  assert.equal(conflict.status, 409);
  assert.equal(conflict.body.error.code, "helper_protocol_mismatch");
  assert.equal(runtime.maxInflight, 2);
  assert.equal(runtime.state, RuntimeState.READY);
});

test("malformed bootstrap stays UNCONFIGURED", async () => {
  const runtime = createHelperRuntime({
    createTransport: async () => {
      throw new Error("must not construct transport");
    },
  });
  const result = await runtime.handleBootstrap(JSON.stringify({ protocol_version: 1, extra: true }));
  assert.equal(result.status, 400);
  assert.equal(runtime.state, RuntimeState.UNCONFIGURED);
  assert.equal(runtime.healthStatus(), 503);
});

test("protocol version mismatch is 409", async () => {
  const runtime = createHelperRuntime();
  const result = await runtime.handleBootstrap(bootstrapBody({ protocol_version: 2 }));
  assert.equal(result.status, 409);
  assert.equal(result.body.error.code, "helper_protocol_mismatch");
  assert.equal(runtime.state, RuntimeState.UNCONFIGURED);
});

test("collection admission rejects before READY and over cap", async () => {
  const runtime = createHelperRuntime({
    createTransport: async () => ({ fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch }),
    createFetchers: () => stubFetchers,
  });
  assert.equal(runtime.refuseCollection()?.body.error.code, "helper_not_ready");
  await runtime.handleBootstrap(bootstrapBody());
  runtime.enterCollection();
  runtime.enterCollection();
  const busy = runtime.refuseCollection();
  assert.equal(busy?.status, 503);
  assert.equal(busy?.body.error.code, "helper_busy");
  runtime.leaveCollection();
  runtime.leaveCollection();
  assert.equal(runtime.refuseCollection(), null);
});

test("parseBootstrapRequest rejects unknown fields", () => {
  assert.throws(
    () => parseBootstrapRequest(JSON.stringify({
      protocol_version: 1,
      limits: { request_body_bytes: 1, response_body_bytes: 1, max_inflight: 1 },
      fingerprint: "nope",
    })),
    /unknown field: fingerprint/,
  );
  assert.throws(
    () => parseBootstrapRequest(JSON.stringify({
      protocol_version: 1,
      proxy: { enabled: false },
      limits: { request_body_bytes: 1, response_body_bytes: 1, max_inflight: 1 },
    })),
    /unknown field: proxy/,
  );
});

test("drain close failure faults runtime", async () => {
  const closed = [];
  const runtime = createHelperRuntime({
    createTransport: async () => ({ fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch }),
    createFetchers: () => ({
      ...stubFetchers,
      async close() {
        closed.push("fetchers");
        throw new Error("fetcher close failed");
      },
    }),
  });
  let stopped = 0;
  let faulted = 0;
  runtime.onStopped = () => { stopped += 1; };
  runtime.onFaulted = () => { faulted += 1; };
  await runtime.handleBootstrap(bootstrapBody());
  runtime.beginDrain();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(closed, ["fetchers"]);
  assert.equal(runtime.state, RuntimeState.FAULTED);
  assert.equal(stopped, 0);
  assert.equal(faulted, 1);
});
