// @ts-check
import { chmodSync } from "node:fs";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { randomUUID } from "node:crypto";
import { createHelperRuntime, RuntimeState } from "./helper-runtime.mjs";
import { createRealFetchers } from "./real-fetchers.mjs";
import {
  handleChannelLiveCheckRequest,
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleVideoLiveCheckRequest,
} from "./rpc-boundary.mjs";
import { rpcErrorResult, rpcErrorResultFor } from "./rpc-execution.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { encodeResponseBody } from "./response-encoding.mjs";

/** @typedef {import("node:http").IncomingMessage} IncomingMessage */
/** @typedef {import("node:http").ServerResponse} ServerResponse */
/** @typedef {import("./contracts.d.ts").FetcherSet} FetcherSet */

class BodyLimitError extends Error {
  constructor() {
    super("request body too large");
    this.name = "BodyLimitError";
    this.code = "request_too_large";
    this.status = 413;
  }
}

class InvalidRequestError extends Error {}
class RequestTimeoutError extends Error {}

const jsonContentTypePattern = /^application\/json(?:\s*;|$)/i;
const contentLengthPattern = /^(0|[1-9][0-9]*)$/;
const positiveIntegerArgPattern = /^[1-9][0-9]*$/;

/**
 * @param {string} socketPath
 * @param {{ requestReadTimeoutMs: number, fetchers?: Partial<FetcherSet>, manageProcess?: boolean }} options
 * @returns {Promise<import("node:http").Server>}
 */
export async function listenUnix(socketPath, options) {
  if (!socketPath) {
    throw new Error("unix socket path is required");
  }
  const opts = normalizeListenOptions(options);
  process.umask(0o077);
  const runtime = createHelperRuntime({
    createFetchers: (fetchImpl, singleAttemptFetchImpl) => (
      { ...createRealFetchers({ fetchImpl, singleAttemptFetchImpl }), ...opts.fetchers }
    ),
  });
  const server = attachHelperServer(runtime, opts.requestReadTimeoutMs);
  if (opts.manageProcess) {
    runtime.onStopped = () => process.exit(0);
    runtime.onFaulted = () => {
      void runtime.closeResources().finally(() => process.exit(1));
    };
    bindSignals(server, runtime);
  }
  await listenWithoutUnlink(server, socketPath);
  return server;
}

/**
 * @param {import("./helper-runtime.mjs").HelperRuntime} runtime
 * @param {number} requestReadTimeoutMs
 */
function attachHelperServer(runtime, requestReadTimeoutMs) {
  /** @type {import("node:http").Server} */
  const server = createServer((req, res) => {
    void handleIncoming(req, res, runtime, server, requestReadTimeoutMs);
  });
  applyServerLimits(server, requestReadTimeoutMs, 0);
  return server;
}

/**
 * @param {IncomingMessage} req
 * @param {ServerResponse} res
 * @param {import("./helper-runtime.mjs").HelperRuntime} runtime
 * @param {import("node:http").Server} server
 * @param {number} requestReadTimeoutMs
 */
async function handleIncoming(req, res, runtime, server, requestReadTimeoutMs) {
  let wrote = false;
  /** @param {number} status @param {unknown} body */
  const send = (status, body) => {
    if (wrote || res.writableEnded || res.destroyed) {
      return;
    }
    wrote = true;
    writeJSON(res, status, body);
  };
  try {
    if (req.method === "GET" && req.url === "/health") {
      send(runtime.healthStatus(), runtime.healthBody());
      return;
    }
    if (req.method === "POST" && req.url === "/v1/bootstrap") {
      const raw = await readBody(req, runtime.requestBodyBytes);
      const result = await runtime.handleBootstrap(raw);
      if (runtime.state === RuntimeState.READY) {
        applyServerLimits(server, requestReadTimeoutMs, runtime.maxInflight);
      }
      send(result.status, result.body);
      return;
    }
    if (isCollectionPath(req)) {
      const denied = runtime.refuseCollection();
      if (denied) {
        req.resume();
        send(denied.status, denied.body);
        return;
      }
      runtime.enterCollection();
      const controller = new AbortController();
      const abortOnRequest = () => controller.abort(new DOMException("aborted", "AbortError"));
      const abortOnResponse = () => {
        if (!res.writableEnded) {
          abortOnRequest();
        }
      };
      req.once("aborted", abortOnRequest);
      res.once("close", abortOnResponse);
      try {
        const raw = await readBody(req, runtime.requestBodyBytes);
        const result = await runWithRequestContext(
          { requestId: randomUUID(), signal: controller.signal },
          () => dispatchCollection(req.url ?? "", raw, runtime.fetchers, runtime.responseBodyBytes),
        );
        send(result.status, result.body);
      } finally {
        req.off("aborted", abortOnRequest);
        res.off("close", abortOnResponse);
        runtime.leaveCollection();
      }
      return;
    }
    const notFound = rpcErrorResult(404, "invalid_request", "PROTOCOL", "unknown endpoint");
    send(notFound.status, notFound.body);
  } catch (err) {
    if (wrote || res.writableEnded || res.destroyed) {
      return;
    }
    if (err instanceof BodyLimitError) {
      res.setHeader("connection", "close");
      const tooLarge = rpcErrorResult(413, "request_too_large", "PROTOCOL", err.message);
      send(tooLarge.status, tooLarge.body);
      res.once("finish", () => res.socket?.destroy());
      return;
    }
    if (err instanceof InvalidRequestError) {
      const invalid = rpcErrorResult(400, "invalid_request", "PROTOCOL", err.message);
      send(invalid.status, invalid.body);
      return;
    }
    if (err instanceof RequestTimeoutError) {
      const timedOut = rpcErrorResult(408, "collection_timeout", "TIMEOUT", "request body timed out");
      send(timedOut.status, timedOut.body);
      return;
    }
    const failure = rpcErrorResultFor(err);
    send(failure.status, failure.body);
  }
}

/**
 * @param {IncomingMessage} req
 */
function isCollectionPath(req) {
  return req.method === "POST" && (
    req.url === "/v1/community" ||
    req.url === "/v1/content" ||
    req.url === "/v1/channel" ||
    req.url === "/v1/channel_live_check" ||
    req.url === "/v1/video_live_check"
  );
}

/**
 * @param {string} url
 * @param {string} raw
 * @param {FetcherSet | null} fetchers
 * @param {number} maximumSuccessResponseBytes
 */
async function dispatchCollection(url, raw, fetchers, maximumSuccessResponseBytes) {
  if (fetchers == null) {
    return rpcErrorResult(503, "helper_not_ready", "PROTOCOL", "helper is not ready");
  }
  if (url === "/v1/community") {
    return handleCommunityRequest(raw, fetchers.fetchCommunity, maximumSuccessResponseBytes);
  }
  if (url === "/v1/content") {
    return handleContentRequest(raw, fetchers.fetchContent, maximumSuccessResponseBytes);
  }
  if (url === "/v1/channel_live_check") {
    return handleChannelLiveCheckRequest(raw, fetchers.fetchChannelLiveCheck, maximumSuccessResponseBytes);
  }
  if (url === "/v1/video_live_check") {
    return handleVideoLiveCheckRequest(raw, fetchers.fetchVideoLiveCheck, maximumSuccessResponseBytes);
  }
  return handleChannelRequest(raw, fetchers.fetchChannel, maximumSuccessResponseBytes);
}

/** @param {ServerResponse} res @param {number} status @param {unknown} body */
function writeJSON(res, status, body) {
  const raw = encodeResponseBody(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(raw),
  });
  res.end(raw);
}

/**
 * @param {IncomingMessage} req
 * @param {number} maxBytes
 * @returns {Promise<string>}
 */
function readBody(req, maxBytes) {
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !jsonContentTypePattern.test(contentType)) {
    return Promise.reject(new InvalidRequestError("content-type must be application/json"));
  }
  const contentLengths = [];
  for (let index = 0; index < req.rawHeaders.length; index += 2) {
    if (req.rawHeaders[index]?.toLowerCase() === "content-length") {
      contentLengths.push(req.rawHeaders[index + 1] ?? "");
    }
  }
  if (contentLengths.length > 1) {
    return Promise.reject(new InvalidRequestError("content-length is invalid"));
  }
  if (contentLengths.length === 1) {
    const rawLength = contentLengths[0];
    if (!contentLengthPattern.test(rawLength)) {
      return Promise.reject(new InvalidRequestError("content-length is invalid"));
    }
    if (Number(rawLength) > maxBytes) {
      req.pause();
      return Promise.reject(new BodyLimitError());
    }
  }
  return new Promise((resolveBody, reject) => {
    /** @type {Buffer[]} */
    const chunks = [];
    let size = 0;
    /** @param {Buffer | string} chunk */
    const onData = (chunk) => {
      const buf = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      size += buf.length;
      if (size > maxBytes) {
        cleanup();
        req.pause();
        reject(new BodyLimitError());
        return;
      }
      chunks.push(buf);
    };
    const onEnd = () => {
      cleanup();
      resolveBody(Buffer.concat(chunks).toString("utf8"));
    };
    /** @param {Error} error */
    const onError = (error) => {
      cleanup();
      reject(error);
    };
    const onAborted = () => {
      cleanup();
      req.pause();
      reject(new DOMException("aborted", "AbortError"));
    };
    const onTimeout = () => {
      cleanup();
      req.pause();
      reject(new RequestTimeoutError("request body timed out"));
    };
    const cleanup = () => {
      req.off("data", onData);
      req.off("end", onEnd);
      req.off("error", onError);
      req.off("aborted", onAborted);
      req.off("timeout", onTimeout);
    };
    req.on("data", onData);
    req.on("end", onEnd);
    req.on("error", onError);
    req.on("aborted", onAborted);
    req.on("timeout", onTimeout);
  });
}

/**
 * @param {import("node:http").Server} server
 * @param {number} requestReadTimeoutMs
 * @param {number} maxInflight
 */
function applyServerLimits(server, requestReadTimeoutMs, maxInflight) {
  server.requestTimeout = requestReadTimeoutMs;
  server.headersTimeout = Math.min(requestReadTimeoutMs, 2_000);
  server.keepAliveTimeout = 2_000;
  server.maxRequestsPerSocket = 1_000;
  if (maxInflight > 0) {
    server.maxConnections = maxInflight + 2;
  }
}

/**
 * @param {import("node:http").Server} server
 * @param {import("./helper-runtime.mjs").HelperRuntime} runtime
 */
function bindSignals(server, runtime) {
  const shutdown = () => {
    runtime.beginDrain();
    server.close();
    if (typeof server.closeIdleConnections === "function") {
      server.closeIdleConnections();
    }
  };
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
}

/**
 * @param {import("node:http").Server} server
 * @param {string} socketPath
 */
async function listenWithoutUnlink(server, socketPath) {
  await new Promise((resolveListen, reject) => {
    /** @param {Error} err */
    const onError = (err) => reject(err);
    server.once("error", onError);
    server.listen(socketPath, () => {
      server.off("error", onError);
      chmodSync(socketPath, 0o700);
      disarmPipeUnlink(server);
      resolveListen(undefined);
    });
  });
}

/** @param {import("node:http").Server} server */
function disarmPipeUnlink(server) {
  // Node net.Server.close()가 _pipeName을 unlink하므로, socket 삭제는 Go만 수행한다.
  if ("_pipeName" in server) {
    server._pipeName = undefined;
  }
}

/**
 * @param {{ requestReadTimeoutMs: number, fetchers?: Partial<FetcherSet>, manageProcess?: boolean }} options
 */
function normalizeListenOptions(options) {
  if (!Number.isSafeInteger(options.requestReadTimeoutMs) || options.requestReadTimeoutMs <= 0) {
    throw new Error("requestReadTimeoutMs must be a positive integer");
  }
  return {
    requestReadTimeoutMs: options.requestReadTimeoutMs,
    fetchers: options.fetchers ?? {},
    manageProcess: Boolean(options.manageProcess),
  };
}

/**
 * Go helper.go가 세 인자를 항상 넘기므로, 빠지거나 형식이 틀린 인자는 대체값 없이 기동 실패로 드러냅니다.
 * @param {string[]} argv
 */
function parseArgs(argv) {
  let socket = "";
  let protocolVersion = "";
  let requestReadTimeoutMs = "";
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--socket") {
      socket = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "--protocol-version") {
      protocolVersion = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "--request-read-timeout-ms") {
      requestReadTimeoutMs = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
  }
  if (socket === "") {
    throw new Error("--socket is required");
  }
  return {
    socket,
    protocolVersion: positiveIntegerArg("--protocol-version", protocolVersion),
    requestReadTimeoutMs: positiveIntegerArg("--request-read-timeout-ms", requestReadTimeoutMs),
  };
}

/**
 * @param {string} name
 * @param {string} raw
 */
function positiveIntegerArg(name, raw) {
  const value = Number(raw);
  if (!positiveIntegerArgPattern.test(raw) || !Number.isSafeInteger(value)) {
    throw new Error(`${name} must be a positive integer`);
  }
  return value;
}

const isMain = process.argv[1] != null && import.meta.url === pathToFileURL(resolve(process.argv[1])).href;
if (isMain) {
  let args;
  try {
    args = parseArgs(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exit(1);
  }
  if (args.protocolVersion !== 1) {
    process.stderr.write("unsupported protocol version\n");
    process.exit(1);
  }
  await listenUnix(args.socket, {
    requestReadTimeoutMs: args.requestReadTimeoutMs,
    manageProcess: true,
  });
}
