// @ts-check
import { createFetchTransport } from "./fetch-transport.mjs";
import { currentRequestSignal } from "./request-context.mjs";
import { rpcErrorResult } from "./rpc-validation.mjs";

export const RuntimeState = Object.freeze({
  UNCONFIGURED: "UNCONFIGURED",
  READY: "READY",
  DRAINING: "DRAINING",
  STOPPED: "STOPPED",
  FAULTED: "FAULTED",
});

const defaultRequestBodyBytes = 64 * 1024;
const minInflight = 1;
const maxInflightBound = 64;

export class HelperHTTPError extends Error {
  /**
   * @param {number} status
   * @param {string} code
   * @param {string} message
   */
  constructor(status, code, message) {
    super(message);
    this.name = "HelperHTTPError";
    this.status = status;
    this.code = code;
  }
}

/**
 * @param {{
 *   createTransport?: () => Promise<{
 *     fetch: import("./upstream-feeds.d.ts").InnertubeFetch,
 *     singleAttemptFetch: import("./upstream-feeds.d.ts").InnertubeFetch,
 *   }>,
 *   createFetchers?: (
 *     fetchImpl: import("./upstream-feeds.d.ts").InnertubeFetch,
 *     singleAttemptFetchImpl: import("./upstream-feeds.d.ts").InnertubeFetch,
 *   ) => import("./contracts.d.ts").FetcherSet,
 * }} [options]
 */
export function createHelperRuntime(options = {}) {
  return new HelperRuntime(options);
}

export class HelperRuntime {
  /**
   * @param {{
   *   createTransport?: () => Promise<{
   *     fetch: import("./upstream-feeds.d.ts").InnertubeFetch,
   *     singleAttemptFetch: import("./upstream-feeds.d.ts").InnertubeFetch,
   *   }>,
   *   createFetchers?: (
   *     fetchImpl: import("./upstream-feeds.d.ts").InnertubeFetch,
   *     singleAttemptFetchImpl: import("./upstream-feeds.d.ts").InnertubeFetch,
   *   ) => import("./contracts.d.ts").FetcherSet,
   * }} [options]
   */
  constructor(options = {}) {
    /** @type {import("./contracts.d.ts").RuntimeState} */
    this.state = RuntimeState.UNCONFIGURED;
    this.inflight = 0;
    this.maxInflight = 0;
    this.requestBodyBytes = defaultRequestBodyBytes;
    this.responseBodyBytes = 1 << 20;
    this.fetchers = null;
    this.fingerprint = "";
    /** @type {Promise<{ status: number, body: unknown }> | null} */
    this.bootstrapPromise = null;
    this.bootstrapAttemptFingerprint = "";
    this.initializing = false;
    /** @type {Promise<{ status: number, body: unknown } | undefined> | null} */
    this.drainPromise = null;
    this.createTransport = options.createTransport ??
      (async () => createFetchTransport({ currentSignal: currentRequestSignal }));
    this.createFetchers = options.createFetchers;
    /** @type {(() => void) | undefined} */
    this.onStopped = undefined;
    /** @type {(() => void) | undefined} */
    this.onFaulted = undefined;
  }

  healthStatus() {
    return this.state === RuntimeState.READY ? 200 : 503;
  }

  healthBody() {
    const proof = this.fetchers?.proofStatus?.();
    return {
      protocol_version: 1,
      state: this.state,
      inflight: this.inflight,
      max_inflight: this.maxInflight,
      ...(proof === undefined ? {} : { proof }),
    };
  }

  /**
   * @returns {{ status: number, body: import("./contracts.d.ts").RPCErrorBody } | null}
   */
  refuseCollection() {
    if (this.state !== RuntimeState.READY) {
      return rpcErrorResult(503, "helper_not_ready", "PROTOCOL", "helper is not ready");
    }
    if (this.inflight >= this.maxInflight) {
      return rpcErrorResult(503, "helper_busy", "TRANSIENT", "helper is busy");
    }
    return null;
  }

  enterCollection() {
    this.inflight += 1;
  }

  leaveCollection() {
    if (this.inflight > 0) {
      this.inflight -= 1;
    }
    if (this.state === RuntimeState.DRAINING && this.inflight === 0) {
      void this.settleDrain();
    }
  }

  /**
   * @param {string} rawBody
   * @returns {Promise<{ status: number, body: unknown }>}
   */
  async handleBootstrap(rawBody) {
    let parsed;
    try {
      parsed = parseBootstrapRequest(rawBody);
    } catch (error) {
      if (error instanceof HelperHTTPError && error.status === 409) {
        return rpcErrorResult(409, "helper_protocol_mismatch", "PROTOCOL", error.message);
      }
      return rpcErrorResult(400, "invalid_request", "PROTOCOL", errorMessage(error));
    }
    const nextFingerprint = bootstrapFingerprint(parsed);
    if (this.state === RuntimeState.READY) {
      if (nextFingerprint === this.fingerprint) {
        return { status: 200, body: this.bootstrapResponse() };
      }
      return rpcErrorResult(409, "helper_protocol_mismatch", "PROTOCOL", "bootstrap config conflict");
    }
    if (this.bootstrapPromise != null) {
      if (nextFingerprint !== this.bootstrapAttemptFingerprint) {
        return rpcErrorResult(409, "helper_protocol_mismatch", "PROTOCOL", "bootstrap config conflict");
      }
      return this.bootstrapPromise;
    }
    if (this.state !== RuntimeState.UNCONFIGURED) {
      return rpcErrorResult(503, "helper_not_ready", "PROTOCOL", "helper is not ready");
    }
    this.bootstrapAttemptFingerprint = nextFingerprint;
    this.bootstrapPromise = this.initializeBootstrap(parsed, nextFingerprint);
    try {
      return await this.bootstrapPromise;
    } finally {
      this.bootstrapPromise = null;
      this.bootstrapAttemptFingerprint = "";
    }
  }

  /**
   * @param {{
   *   protocol_version: number,
   *   limits: { request_body_bytes: number, response_body_bytes: number, max_inflight: number },
   * }} parsed
   * @param {string} nextFingerprint
   * @returns {Promise<{ status: number, body: unknown }>}
   */
  async initializeBootstrap(parsed, nextFingerprint) {
    this.initializing = true;
    try {
      const transport = await this.createTransport();
      const fetchers = this.createFetchers
        ? this.createFetchers(
          /** @type {import("./upstream-feeds.d.ts").InnertubeFetch} */ (transport.fetch),
          /** @type {import("./upstream-feeds.d.ts").InnertubeFetch} */ (transport.singleAttemptFetch),
        )
        : null;
      this.fetchers = fetchers;
      this.initializing = false;
      // 초기화 중 들어온 종료 요청은 늦게 생성된 자원을 닫은 뒤 완료합니다.
      if (this.state === RuntimeState.DRAINING) {
        const failure = await this.settleDrain();
        return failure ?? rpcErrorResult(503, "helper_not_ready", "PROTOCOL", "helper is not ready");
      }
      this.requestBodyBytes = parsed.limits.request_body_bytes;
      this.responseBodyBytes = parsed.limits.response_body_bytes;
      this.maxInflight = parsed.limits.max_inflight;
      this.fingerprint = nextFingerprint;
      this.state = RuntimeState.READY;
      return { status: 200, body: this.bootstrapResponse() };
    } catch (error) {
      try {
        await this.closeResources();
      } catch {}
      this.state = RuntimeState.FAULTED;
      this.onFaulted?.();
      return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", errorMessage(error));
    } finally {
      this.initializing = false;
    }
  }

  bootstrapResponse() {
    return {
      protocol_version: 1,
      state: this.state,
      request_body_bytes: this.requestBodyBytes,
      response_body_bytes: this.responseBodyBytes,
      max_inflight: this.maxInflight,
    };
  }

  beginDrain() {
    if (this.state === RuntimeState.UNCONFIGURED && !this.initializing) {
      this.state = RuntimeState.STOPPED;
      this.onStopped?.();
      return;
    }
    if (this.state !== RuntimeState.READY && this.state !== RuntimeState.UNCONFIGURED) {
      return;
    }
    this.state = RuntimeState.DRAINING;
    if (this.inflight === 0) {
      void this.settleDrain();
    }
  }

  async settleDrain() {
    if (this.state !== RuntimeState.DRAINING || this.initializing || this.inflight > 0) {
      return;
    }
    if (this.drainPromise === null) {
      // close에서 재진입해도 정리와 종료 통보를 한 번만 수행하도록 먼저 작업을 공유합니다.
      this.drainPromise = Promise.resolve().then(async () => {
        try {
          await this.closeResources();
          this.state = RuntimeState.STOPPED;
          this.onStopped?.();
        } catch (error) {
          this.state = RuntimeState.FAULTED;
          this.onFaulted?.();
          return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", errorMessage(error));
        }
      });
    }
    return this.drainPromise;
  }

  async closeResources() {
    const fetchers = this.fetchers;
    this.fetchers = null;
    if (fetchers && typeof fetchers.close === "function") {
      await fetchers.close();
    }
  }
}

/**
 * @param {string} rawBody
 * @returns {{
 *   protocol_version: number,
 *   limits: { request_body_bytes: number, response_body_bytes: number, max_inflight: number },
 * }}
 */
export function parseBootstrapRequest(rawBody) {
  let value;
  try {
    value = JSON.parse(rawBody || "{}");
  } catch {
    throw new HelperHTTPError(400, "invalid_request", "request is not JSON");
  }
  if (!isRecord(value)) {
    throw new HelperHTTPError(400, "invalid_request", "request must be a JSON object");
  }
  assertExactKeys(value, bootstrapKeys);
  if (value.protocol_version !== 1) {
    throw new HelperHTTPError(409, "helper_protocol_mismatch", "protocol version mismatch");
  }
  if (!isRecord(value.limits)) {
    throw new HelperHTTPError(400, "invalid_request", "limits must be an object");
  }
  assertExactKeys(value.limits, bootstrapLimitKeys);
  return {
    protocol_version: 1,
    limits: {
      request_body_bytes: positiveInt(value.limits, "request_body_bytes"),
      response_body_bytes: positiveInt(value.limits, "response_body_bytes"),
      max_inflight: boundedInt(value.limits, "max_inflight", minInflight, maxInflightBound),
    },
  };
}

// bootstrap key는 모두 필수이므로 허용 집합의 삽입 순서가 곧 누락 검사 순서입니다.
const bootstrapKeys = new Set(["protocol_version", "limits"]);
const bootstrapLimitKeys = new Set(["request_body_bytes", "response_body_bytes", "max_inflight"]);

/**
 * unknown key를 record 순서대로 먼저 거부한 뒤 필수 key 누락을 거부합니다.
 * @param {Record<string, unknown>} record
 * @param {ReadonlySet<string>} requiredKeys
 */
function assertExactKeys(record, requiredKeys) {
  for (const key in record) {
    if (Object.hasOwn(record, key) && !requiredKeys.has(key)) {
      throw new HelperHTTPError(400, "invalid_request", `unknown field: ${key}`);
    }
  }
  for (const key of requiredKeys) {
    if (!Object.hasOwn(record, key)) {
      throw new HelperHTTPError(400, "invalid_request", `${key} is required`);
    }
  }
}

/**
 * @param {{
 *   protocol_version: number,
 *   limits: { request_body_bytes: number, response_body_bytes: number, max_inflight: number },
 * }} config
 */
function bootstrapFingerprint(config) {
  return JSON.stringify({
    protocol_version: config.protocol_version,
    limits: config.limits,
  });
}

/**
 * @param {Record<string, unknown>} record
 * @param {string} field
 */
function positiveInt(record, field) {
  return boundedInt(record, field, 1, Number.MAX_SAFE_INTEGER);
}

/**
 * @param {Record<string, unknown>} record
 * @param {string} field
 * @param {number} min
 * @param {number} max
 */
function boundedInt(record, field, min, max) {
  const value = record[field];
  if (!Number.isSafeInteger(value) || Number(value) < min || Number(value) > max) {
    throw new HelperHTTPError(400, "invalid_request", `${field} is invalid`);
  }
  return Number(value);
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** @param {unknown} error */
function errorMessage(error) {
  return error instanceof Error ? error.message : "helper error";
}
