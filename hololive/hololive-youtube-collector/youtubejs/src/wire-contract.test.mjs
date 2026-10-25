import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { encodeResponseBody } from "./response-encoding.mjs";
import { channelEndpoint, communityEndpoint, contentEndpoint } from "./rpc-collection-validation.mjs";
import { RpcProtocolError, RpcResponseError } from "./rpc-errors.mjs";
import { rpcErrorResult, rpcErrorResultFor } from "./rpc-execution.mjs";
import { channelLiveCheckEndpoint, videoLiveCheckEndpoint } from "./rpc-live-check-validation.mjs";
import { failureTuples } from "./upstream-errors.mjs";

// Go 클라이언트 테스트와 같은 fixture로 helper wire 계약의 실제 검증·오류 생성 동작을 확인합니다.
const fixture = JSON.parse(readFileSync(new URL("../testdata/wire-contract.json", import.meta.url), "utf8"));

const endpoints = {
  "/v1/community": communityEndpoint,
  "/v1/content": contentEndpoint,
  "/v1/channel": channelEndpoint,
  "/v1/channel_live_check": channelLiveCheckEndpoint,
  "/v1/video_live_check": videoLiveCheckEndpoint,
};

const allStatuses = [...new Set(Object.values(fixture.failure_tuples).flatMap((tuple) => tuple.statuses))];

test("wire fixture covers every helper failure code", () => {
  assert.deepEqual(Object.keys(failureTuples).sort(), Object.keys(fixture.failure_tuples).sort());
});

test("rpcErrorResult accepts exactly the fixture status and class tuples", () => {
  for (const [code, tuple] of Object.entries(fixture.failure_tuples)) {
    for (const status of allStatuses) {
      const build = () => rpcErrorResult(status, code, tuple.class, "failure");
      if (tuple.statuses.includes(status)) {
        assert.equal(build().status, status, `${code} ${status}`);
      } else {
        assert.throws(build, /invalid RPC failure tuple/, `${code} ${status}`);
      }
    }
    const otherClass = tuple.class === "INTERNAL" ? "PROTOCOL" : "INTERNAL";
    assert.throws(() => rpcErrorResult(tuple.statuses[0], code, otherClass, "failure"), /invalid RPC failure tuple/);
  }
});

test("classified failures only emit fixture retry kinds", () => {
  const hints = [
    { kind: "default" },
    { kind: "after", after_ms: 1500 },
    { kind: "at", at: "2099-01-02T03:04:05Z" },
  ];
  for (const [code, tuple] of Object.entries(fixture.failure_tuples)) {
    for (const retry of hints) {
      const { status, body } = rpcErrorResultFor(Object.assign(new Error("failure"), { code, retry }));
      const emitted = fixture.failure_tuples[body.error.code];
      assert.ok(emitted.statuses.includes(status), `${code} ${retry.kind} status ${status}`);
      assert.equal(body.error.class, emitted.class);
      assert.ok(emitted.retry_kinds.includes(body.error.retry.kind), `${code} ${retry.kind} emitted ${body.error.retry.kind}`);
      if (body.error.code === code && tuple.retry_kinds.includes(retry.kind)) {
        assert.equal(body.error.retry.kind, retry.kind, `${code} must keep a permitted ${retry.kind} hint`);
      }
    }
  }
});

for (const [path, cases] of Object.entries(fixture.endpoints)) {
  const endpoint = endpoints[path];

  test(`${path} minimum success size is the smallest fixture minimal body`, () => {
    const sizes = cases.minimal.map((body) => Buffer.byteLength(encodeResponseBody(body)));
    assert.equal(endpoint.minimumSuccessResponseBytes, Math.min(...sizes));
  });

  test(`${path} validator accepts fixture success bodies unchanged`, () => {
    for (const item of cases.valid) {
      assert.deepEqual(endpoint.validateResponse(structuredClone(item.body)), item.body, item.name);
    }
  });

  test(`${path} validator rejects fixture invalid bodies`, () => {
    for (const item of cases.invalid) {
      // 검증기 자체의 결함(TypeError 등)이 거부로 통과하지 않도록 응답·protocol 계약 오류만 인정합니다.
      assert.throws(
        () => endpoint.validateResponse(structuredClone(item.body)),
        (error) => error instanceof RpcResponseError || error instanceof RpcProtocolError,
        item.name,
      );
    }
  });
}
