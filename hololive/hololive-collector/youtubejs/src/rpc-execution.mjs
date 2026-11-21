// @ts-check

import { classifyUpstreamError, failureTuples } from "./upstream-errors.mjs";
import { encodeResponseBody } from "./response-encoding.mjs";
import { RpcResponseError } from "./rpc-errors.mjs";
import { isRecord } from "./rpc-validation-primitives.mjs";

/**
 * @template Request
 * @template Response
 * @param {string} rawBody
 * @param {import("./contracts.d.ts").RpcEndpoint<Request, Response>} endpoint
 * @param {(request: Request) => Promise<unknown>} run
 * @param {number} [maximumSuccessResponseBytes]
 * @returns {Promise<{ status: number, body: Response | import("./contracts.d.ts").RPCErrorBody }>}
 */
export async function handleRpcRequest(rawBody, endpoint, run, maximumSuccessResponseBytes = Number.MAX_SAFE_INTEGER) {
  const parsed = parseRpcRequest(rawBody, endpoint.validateRequest);
  if (parsed.ok === false) {
    return rpcErrorResult(400, "invalid_request", "PROTOCOL", parsed.error);
  }
  const requestedLimit = Number(
    /** @type {{ max_success_response_bytes: number }} */ (parsed.value).max_success_response_bytes,
  );
  if (requestedLimit > maximumSuccessResponseBytes) {
    return rpcErrorResult(400, "invalid_request", "PROTOCOL", "max_success_response_bytes exceeds bootstrap limit");
  }
  if (requestedLimit < endpoint.minimumSuccessResponseBytes) {
    return rpcErrorResult(422, "response_too_large", "RESOURCE_LIMIT", "success response metadata exceeds requested limit");
  }
  try {
    const raw = await run(parsed.value);
    if (!isRecord(raw)) {
      throw new RpcResponseError("response must be a JSON object");
    }
    const body = endpoint.validateResponse({ protocol_version: 1, ...raw });
    const encoded = Buffer.byteLength(encodeResponseBody(body));
    if (encoded > requestedLimit) {
      return rpcErrorResult(422, "response_too_large", "RESOURCE_LIMIT", "success response exceeds requested limit");
    }
    return { status: 200, body };
  } catch (error) {
    return rpcErrorResultFor(error);
  }
}

/** @param {unknown} error */
export function rpcErrorResultFor(error) {
  const code = classifyUpstreamError(error);
  if (code === "collection_canceled") {
    return rpcErrorResult(408, code, "CANCELED", "collection canceled");
  }
  const tuple = failureTuples[code];
  const result = rpcErrorResult(tuple.statuses[0], code, tuple.class, safeErrorMessage(error));
  if ((code === "cooldown" || code === "helper_busy") && isRecord(error) && isRecord(error.retry)) {
    const retry = error.retry;
    if (retry.kind === "after" && Number.isSafeInteger(retry.after_ms) && Number(retry.after_ms) > 0) {
      Object.assign(result.body.error.retry, { kind: "after", after_ms: Number(retry.after_ms) });
    } else if (code === "cooldown" && retry.kind === "at" && typeof retry.at === "string" && Number.isFinite(Date.parse(retry.at))) {
      Object.assign(result.body.error.retry, { kind: "at", at: retry.at });
    } else if (retry.kind !== "default") {
      return rpcErrorResult(500, "helper_internal_invariant", "INTERNAL", "upstream retry hint is invalid");
    }
  }
  return result;
}

/**
 * @param {number} status
 * @param {import("./contracts.d.ts").RPCErrorCode} code
 * @param {import("./contracts.d.ts").RPCFailureClass} failureClass
 * @param {string} message
 */
export function rpcErrorResult(status, code, failureClass, message) {
  const tuple = failureTuples[code];
  if (tuple == null || tuple.class !== failureClass || !tuple.statuses.includes(status)) {
    throw Object.assign(new Error("invalid RPC failure tuple"), { code: "helper_internal_invariant" });
  }
  return {
    status,
    body: {
      protocol_version: 1,
      error: {
        code,
        class: failureClass,
        retry: { kind: /** @type {const} */ ("default") },
        message: boundedMessage(message),
      },
    },
  };
}

/**
 * @template T
 * @param {string} rawBody
 * @param {(value: unknown) => T} validate
 * @returns {{ ok: true, value: T } | { ok: false, error: string }}
 */
function parseRpcRequest(rawBody, validate) {
  /** @type {unknown} */
  let value;
  try {
    value = /** @type {unknown} */ (JSON.parse(rawBody || "{}"));
  } catch {
    return { ok: false, error: "request is not JSON" };
  }
  try {
    return { ok: true, value: validate(value) };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

/** @param {unknown} error */
function safeErrorMessage(error) {
  const raw = error instanceof Error ? error.message : "helper error";
  return boundedMessage(raw);
}

/** @param {unknown} error */
function errorMessage(error) {
  return error instanceof Error ? boundedMessage(error.message) : "request validation failed";
}

/** @param {string} message */
function boundedMessage(message) {
  const redacted = message
    .replace(/\/\/[^/\s@]+@/g, "//redacted@")
    .replace(/(authorization|x-apikey|cookie)\s*[:=]\s*\S+/gi, "$1=[redacted]")
    .replace(/([?&](?:token|key|secret|password)=)[^&\s]+/gi, "$1[redacted]");
  const raw = Buffer.from(redacted, "utf8");
  if (raw.length <= 512) {
    return redacted;
  }
  let end = 512;
  while (end > 0 && (raw[end] & 0xc0) === 0x80) {
    end -= 1;
  }
  return raw.subarray(0, end).toString("utf8");
}
