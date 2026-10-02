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

test("drain waits for pending bootstrap resources before rejecting shared bootstraps", async () => {
  const transportReady = Promise.withResolvers();
  const closeStarted = Promise.withResolvers();
  const closeFinished = Promise.withResolvers();
  const events = [];
  const runtime = createHelperRuntime({
    createTransport: async () => {
      await transportReady.promise;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => {
      events.push("created");
      return {
        ...stubFetchers,
        async close() {
          events.push("closing");
          closeStarted.resolve();
          await closeFinished.promise;
          events.push("closed");
        },
      };
    },
  });
  runtime.onStopped = () => events.push("stopped");
  runtime.onFaulted = () => events.push("faulted");
  const first = runtime.handleBootstrap(bootstrapBody()).then((result) => {
    events.push("bootstrap returned");
    return result;
  });
  const second = runtime.handleBootstrap(bootstrapBody());
  runtime.beginDrain();
  runtime.beginDrain();
  await runtime.settleDrain();
  assert.equal(runtime.state, RuntimeState.DRAINING);
  assert.equal(runtime.healthStatus(), 503);
  assert.deepEqual(events, []);

  transportReady.resolve();
  await closeStarted.promise;
  runtime.beginDrain();
  assert.equal(runtime.state, RuntimeState.DRAINING);
  assert.equal(runtime.refuseCollection()?.body.error.code, "helper_not_ready");
  assert.deepEqual(events, ["created", "closing"]);
  closeFinished.resolve();
  const results = await Promise.all([first, second]);
  assert.deepEqual(results, [results[0], results[0]]);
  assert.equal(results[0].status, 503);
  assert.equal(results[0].body.error.code, "helper_not_ready");
  assert.equal(runtime.state, RuntimeState.STOPPED);
  assert.equal(runtime.fetchers, null);
  assert.equal(runtime.maxInflight, 0);
  assert.deepEqual(events, ["created", "closing", "closed", "stopped", "bootstrap returned"]);
  assert.equal((await runtime.handleBootstrap(bootstrapBody())).status, 503);
});

for (const closeFails of [false, true]) {
  test(`duplicate drain waits for one resource close (${closeFails ? "failure" : "success"})`, async () => {
    const closeStarted = Promise.withResolvers();
    const closeFinished = Promise.withResolvers();
    const events = [];
    const runtime = createHelperRuntime({
      createTransport: async () => ({ fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch }),
      createFetchers: () => ({
        ...stubFetchers,
        async close() {
          events.push("closing");
          closeStarted.resolve();
          await closeFinished.promise;
          if (closeFails) throw new Error("fetcher close failed");
          events.push("closed");
        },
      }),
    });
    runtime.onStopped = () => events.push("stopped");
    runtime.onFaulted = () => events.push("faulted");
    await runtime.handleBootstrap(bootstrapBody());
    runtime.enterCollection();
    runtime.beginDrain();
    runtime.beginDrain();
    await runtime.settleDrain();
    assert.deepEqual(events, []);
    runtime.leaveCollection();
    await closeStarted.promise;
    runtime.leaveCollection();
    const repeatedDrain = runtime.settleDrain();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(runtime.state, RuntimeState.DRAINING);
    assert.deepEqual(events, ["closing"]);
    closeFinished.resolve();
    await repeatedDrain;
    assert.equal(runtime.state, closeFails ? RuntimeState.FAULTED : RuntimeState.STOPPED);
    assert.deepEqual(events, closeFails ? ["closing", "faulted"] : ["closing", "closed", "stopped"]);
    runtime.beginDrain();
    await runtime.settleDrain();
    assert.deepEqual(events, closeFails ? ["closing", "faulted"] : ["closing", "closed", "stopped"]);
  });
}

test("late bootstrap resource close failure returns INTERNAL and faults once", async () => {
  const transportReady = Promise.withResolvers();
  const events = [];
  const runtime = createHelperRuntime({
    createTransport: async () => {
      await transportReady.promise;
      return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
    },
    createFetchers: () => ({
      ...stubFetchers,
      async close() {
        events.push("closing");
        throw new Error("late resource close failed");
      },
    }),
  });
  runtime.onStopped = () => events.push("stopped");
  runtime.onFaulted = () => events.push("faulted");
  const pending = runtime.handleBootstrap(bootstrapBody());
  runtime.beginDrain();
  transportReady.resolve();
  const result = await pending;
  assert.equal(result.status, 500);
  assert.equal(result.body.error.code, "helper_internal_invariant");
  assert.equal(result.body.error.message, "late resource close failed");
  assert.equal(runtime.state, RuntimeState.FAULTED);
  assert.equal(runtime.fetchers, null);
  assert.deepEqual(events, ["closing", "faulted"]);
});

for (const failAt of ["transport", "fetchers"]) {
  for (const drain of [false, true]) {
    test(`bootstrap ${failAt} failure stays FAULTED${drain ? " during drain" : ""}`, async () => {
      const transportReady = Promise.withResolvers();
      const events = [];
      const runtime = createHelperRuntime({
        createTransport: async () => {
          await transportReady.promise;
          if (failAt === "transport") throw new Error("transport initialization failed");
          return { fetch: globalThis.fetch, singleAttemptFetch: globalThis.fetch };
        },
        createFetchers: () => {
          throw new Error("fetchers initialization failed");
        },
      });
      runtime.onStopped = () => events.push("stopped");
      runtime.onFaulted = () => events.push("faulted");
      const pending = runtime.handleBootstrap(bootstrapBody());
      if (drain) runtime.beginDrain();
      transportReady.resolve();
      const result = await pending;
      assert.equal(result.status, 500);
      assert.equal(result.body.error.code, "helper_internal_invariant");
      assert.equal(result.body.error.message, `${failAt} initialization failed`);
      assert.equal(runtime.state, RuntimeState.FAULTED);
      assert.equal(runtime.fetchers, null);
      assert.equal(runtime.healthStatus(), 503);
      assert.deepEqual(events, ["faulted"]);
      runtime.beginDrain();
      await runtime.closeResources();
      assert.equal((await runtime.handleBootstrap(bootstrapBody())).status, 503);
      assert.deepEqual(events, ["faulted"]);
    });
  }
}

test("drain before bootstrap stops once without initializing resources", async () => {
  const events = [];
  const runtime = createHelperRuntime({
    createTransport: async () => {
      throw new Error("must not construct transport");
    },
  });
  runtime.onStopped = () => events.push("stopped");
  runtime.beginDrain();
  runtime.beginDrain();
  assert.equal(runtime.state, RuntimeState.STOPPED);
  assert.equal((await runtime.handleBootstrap(bootstrapBody())).status, 503);
  assert.deepEqual(events, ["stopped"]);
});
