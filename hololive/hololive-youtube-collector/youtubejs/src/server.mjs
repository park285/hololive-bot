// @ts-check
import { chmodSync } from "node:fs";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { randomUUID } from "node:crypto";
import { emptyCommunityPage } from "./fetch-community.mjs";
import { createHelperRuntime, RuntimeState } from "./helper-runtime.mjs";
import { createRealFetchers, stubFetchers } from "./real-fetchers.mjs";
import {
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
} from "./rpc-boundary.mjs";
import { rpcErrorResult, rpcErrorResultFor } from "./rpc-validation.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { encodeResponseBody } from "./response-encoding.mjs";

export {
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
  RuntimeState,
};

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

/**
 * @param {Partial<FetcherSet>} [overrides]
 * @returns {import("node:http").Server}
 */
export function createHelperServer(overrides = {}) {
  const runtime = createHelperRuntime({
    createFetchers: (fetchImpl) => ({ ...createRealFetchers({ fetchImpl }), ...overrides }),
    transportCloseTimeoutMs: 3_000,
  });
  return attachHelperServer(runtime, 30_000);
}

/**
 * @param {string} socketPath
 * @param {Partial<FetcherSet> | { stub?: boolean, requestReadTimeoutMs?: number, transportCloseTimeoutMs?: number, fetchers?: Partial<FetcherSet>, manageProcess?: boolean }} [options]
 * @returns {Promise<import("node:http").Server>}
 */
export async function listenUnix(socketPath, options = {}) {
  if (!socketPath) {
    throw new Error("unix socket path is required");
  }
  process.umask(0o077);
  const opts = normalizeListenOptions(options);
  const runtime = createHelperRuntime({
    createFetchers: (fetchImpl) => {
      if (opts.stub) {
        return stubFetchers;
      }
      return { ...createRealFetchers({ fetchImpl }), ...opts.fetchers };
    },
    transportCloseTimeoutMs: opts.transportCloseTimeoutMs,
  });
  const server = attachHelperServer(runtime, opts.requestReadTimeoutMs);
  applyServerLimits(server, opts.requestReadTimeoutMs, 0);
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
    req.url === "/v1/viewer"
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
  if (url === "/v1/channel") {
    return handleChannelRequest(raw, fetchers.fetchChannel, maximumSuccessResponseBytes);
  }
  return handleViewerRequest(raw, fetchers.fetchViewer, maximumSuccessResponseBytes);
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
 * @param {number} limit
 * @returns {Promise<string>}
 */
function readBody(req, limit) {
  const maxBytes = limit > 0 ? limit : defaultRequestBodyBytes();
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !/^application\/json(?:\s*;|$)/i.test(contentType)) {
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
    if (!/^(0|[1-9][0-9]*)$/.test(rawLength)) {
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

function defaultRequestBodyBytes() {
  return 64 * 1024;
}

/**
 * @param {import("node:http").Server} server
 * @param {number} requestReadTimeoutMs
 * @param {number} maxInflight
 */
function applyServerLimits(server, requestReadTimeoutMs, maxInflight) {
  const timeout = requestReadTimeoutMs > 0 ? requestReadTimeoutMs : 30_000;
  server.requestTimeout = timeout;
  server.headersTimeout = Math.min(timeout, 2_000);
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
 * @param {Partial<FetcherSet> | { stub?: boolean, requestReadTimeoutMs?: number, transportCloseTimeoutMs?: number, fetchers?: Partial<FetcherSet>, manageProcess?: boolean }} options
 */
function normalizeListenOptions(options) {
  if (options && typeof options === "object" && "fetchCommunity" in options) {
    return {
      stub: false,
      requestReadTimeoutMs: 30_000,
      transportCloseTimeoutMs: 3_000,
      fetchers: options,
      manageProcess: false,
    };
  }
  const record = options && typeof options === "object" ? options : {};
  return {
    stub: Boolean("stub" in record && record.stub),
    requestReadTimeoutMs: "requestReadTimeoutMs" in record && typeof record.requestReadTimeoutMs === "number"
      ? record.requestReadTimeoutMs
      : 30_000,
    transportCloseTimeoutMs: "transportCloseTimeoutMs" in record && typeof record.transportCloseTimeoutMs === "number"
      ? record.transportCloseTimeoutMs
      : 3_000,
    fetchers: "fetchers" in record && record.fetchers ? record.fetchers : {},
    manageProcess: Boolean("manageProcess" in record && record.manageProcess),
  };
}

/** @param {string[]} argv */
function parseArgs(argv) {
  const args = {
    socket: "",
    stub: false,
    protocolVersion: 1,
    requestReadTimeoutMs: 30_000,
    transportCloseTimeoutMs: 3_000,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--socket") {
      args.socket = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "--protocol-version") {
      args.protocolVersion = Number(argv[i + 1] ?? 1);
      i += 1;
      continue;
    }
    if (arg === "--request-read-timeout-ms") {
      args.requestReadTimeoutMs = Number(argv[i + 1] ?? 30_000);
      i += 1;
      continue;
    }
    if (arg === "--shutdown-timeout-ms") {
      args.transportCloseTimeoutMs = Number(argv[i + 1] ?? 3_000);
      i += 1;
      continue;
    }
    if (arg === "--stub") {
      args.stub = true;
    }
  }
  return args;
}

const isMain = process.argv[1] != null && import.meta.url === pathToFileURL(resolve(process.argv[1])).href;
if (isMain) {
  const args = parseArgs(process.argv.slice(2));
  if (args.protocolVersion !== 1) {
    process.stderr.write("unsupported protocol version\n");
    process.exit(1);
  }
  if (!Number.isSafeInteger(args.transportCloseTimeoutMs) || args.transportCloseTimeoutMs <= 0) {
    process.stderr.write("invalid shutdown timeout\n");
    process.exit(1);
  }
  process.umask(0o077);
  await listenUnix(args.socket, {
    stub: args.stub,
    requestReadTimeoutMs: args.requestReadTimeoutMs,
    transportCloseTimeoutMs: args.transportCloseTimeoutMs,
    manageProcess: true,
  });
}

export { emptyCommunityPage };
