import assert from "node:assert/strict";
import test from "node:test";

import { createFetchTransport } from "./fetch-transport.mjs";
import { currentRequestSignal, runWithRequestContext } from "./request-context.mjs";
import {
  communityEndpoint,
  handleRpcRequest,
  rpcErrorResult,
  rpcErrorResultFor,
  validateCommunityResponse,
} from "./rpc-validation.mjs";

test("rpcErrorResult accepts the closed status and class tuples", () => {
  assert.equal(rpcErrorResult(404, "invalid_request", "PROTOCOL", "unknown endpoint").status, 404);
  assert.equal(rpcErrorResult(408, "collection_timeout", "TIMEOUT", "request body timed out").status, 408);
  assert.equal(rpcErrorResult(504, "collection_timeout", "TIMEOUT", "upstream timed out").status, 504);
});

test("rpcErrorResult rejects an impossible status and body tuple", () => {
  assert.throws(
    () => rpcErrorResult(500, "collection_failed", "TRANSIENT", "failed"),
    /invalid RPC failure tuple/,
  );
});

test("PAG-013 response validator requires termination_reason and rejects impossible tuples", () => {
  const base = {
    protocol_version: 1,
    posts: [],
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
  assert.throws(() => validateCommunityResponse(base), /termination_reason is required/);
  assert.throws(
    () => validateCommunityResponse({
      ...base,
      exhausted: false,
      termination_reason: "exhausted",
    }),
    (error) => error.code === "helper_protocol_mismatch",
  );
});

test("PAG-012 response validator rejects a 8193-byte cursor", () => {
  const page = {
    protocol_version: 1,
    posts: [],
    page_count: 1,
    exhausted: false,
    continuity: "GAP_UNRESOLVED",
    termination_reason: "max_pages",
    cursor_start: "x".repeat(8190),
  };
  assert.equal(validateCommunityResponse(page).cursor_start, page.cursor_start);
  assert.throws(
    () => validateCommunityResponse({ ...page, cursor_start: `${page.cursor_start}x` }),
    (error) => error.code === "helper_protocol_mismatch",
  );
});

test("PAG-004 cooldown and configuration failures remain fatal with retry metadata", () => {
  const cooldown = new Error("limited");
  cooldown.status = 429;
  cooldown.retry = { kind: "after", after_ms: 30_000 };
  const cooldownResult = rpcErrorResultFor(cooldown);
  assert.equal(cooldownResult.status, 429);
  assert.deepEqual(cooldownResult.body.error.retry, { kind: "after", after_ms: 30_000 });

  const forbidden = new Error("forbidden");
  forbidden.status = 403;
  const forbiddenResult = rpcErrorResultFor(forbidden);
  assert.equal(forbiddenResult.body.error.code, "configuration_error");
  assert.equal(forbiddenResult.body.error.class, "CONFIGURATION");
});

test("canceled requests do not copy abort reason into the error body", async () => {
  const secret = "secret-init-abort-reason";
  const controller = new AbortController();
  controller.abort(new Error(secret));
  const result = await runWithRequestContext(
    { requestId: "rpc-abort", signal: controller.signal },
    () => handleRpcRequest(
      JSON.stringify({
        protocol_version: 1,
        channel_id: "UC_TEST",
        max_success_response_bytes: 1048576,
      }),
      communityEndpoint,
      async () => {
        throw controller.signal.reason;
      },
    ),
  );
  assert.equal(result.status, 408);
  assert.equal(result.body.error.code, "collection_canceled");
  assert.equal(result.body.error.class, "CANCELED");
  assert.equal(result.body.error.message, "collection canceled");
  assert.equal(JSON.stringify(result.body).includes(secret), false);
});

test("AbortError without request provenance fail-closes as an internal invariant", () => {
  const secret = "secret-init-abort-reason";
  const error = new Error(secret);
  error.name = "AbortError";
  const result = rpcErrorResultFor(error);
  assert.equal(result.status, 500);
  assert.equal(result.body.error.code, "helper_internal_invariant");
});

test("RequestInit abort is not misclassified as parent request cancellation", async () => {
  const secret = "secret-init-abort-reason";
  const rpcController = new AbortController();
  const initController = new AbortController();
  initController.abort(new Error(secret));
  const transport = await createFetchTransport({
    proxy: { enabled: false },
    currentSignal: () => currentRequestSignal(),
  });
  try {
    const result = await runWithRequestContext(
      { requestId: "live-rpc", signal: rpcController.signal },
      () => handleRpcRequest(
        JSON.stringify({
          protocol_version: 1,
          channel_id: "UC_TEST",
          max_success_response_bytes: 1048576,
        }),
        communityEndpoint,
        async () => {
          await transport.fetch("http://127.0.0.1/", { signal: initController.signal });
          return {};
        },
      ),
    );
    assert.equal(result.status, 500);
    assert.equal(result.body.error.code, "helper_internal_invariant");
    assert.equal(JSON.stringify(result.body).includes(secret), false);
    assert.equal(rpcController.signal.aborted, false);
  } finally {
    await transport.close();
  }
});

test("untyped errors fail-close instead of becoming transient collection failures", () => {
  const result = rpcErrorResultFor(new Error("programming failure"));
  assert.equal(result.status, 500);
  assert.equal(result.body.error.code, "helper_internal_invariant");
  assert.equal(result.body.error.class, "INTERNAL");
});
