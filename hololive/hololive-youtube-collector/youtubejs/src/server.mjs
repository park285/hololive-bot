// @ts-check
import { createServer } from "node:http";
import { unlinkSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { emptyCommunityPage } from "./fetch-community.mjs";
import { realFetchers, setProxyUrl, stubFetchers } from "./real-fetchers.mjs";
import {
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
} from "./rpc-boundary.mjs";
import { rpcErrorBody } from "./rpc-validation.mjs";

export {
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
  setProxyUrl,
};

/** @typedef {import("node:http").IncomingMessage} IncomingMessage */
/** @typedef {import("node:http").ServerResponse} ServerResponse */
/** @typedef {import("./contracts.d.ts").FetcherSet} FetcherSet */

const maxBodyBytes = 64 * 1024;

/** @type {(err: unknown) => import("./contracts.d.ts").HelperErrorBody} */
function helperError(err) {
  return rpcErrorBody(err);
}

/** @type {(overrides?: Partial<FetcherSet>) => import("node:http").Server} */
export function createHelperServer(overrides = {}) {
  const fetchers = { ...realFetchers, ...overrides };
  return createServer(async (req, res) => {
    try {
      if (req.method === "GET" && req.url === "/health") {
        writeJSON(res, 200, { ok: true });
        return;
      }
      if (req.method === "POST" && req.url === "/v1/community") {
        const result = await handleCommunityRequest(await readBody(req), fetchers.fetchCommunity, setProxyUrl);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/content") {
        const result = await handleContentRequest(await readBody(req), fetchers.fetchContent, setProxyUrl);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/channel") {
        const result = await handleChannelRequest(await readBody(req), fetchers.fetchChannel, setProxyUrl);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/viewer") {
        const result = await handleViewerRequest(await readBody(req), fetchers.fetchViewer, setProxyUrl);
        writeJSON(res, result.status, result.body);
        return;
      }
      writeJSON(res, 404, { error: "not found", error_code: "collection_failed" });
    } catch (err) {
      writeJSON(res, 500, helperError(err));
    }
  });
}

/** @type {(res: ServerResponse, status: number, body: unknown) => void} */
function writeJSON(res, status, body) {
  const raw = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(raw),
  });
  res.end(raw);
}

/** @type {(req: IncomingMessage) => Promise<string>} */
function readBody(req) {
  return new Promise((resolveBody, reject) => {
    /** @type {Buffer[]} */
    const chunks = [];
    let size = 0;
    req.on("data", (chunk) => {
      const buf = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      size += buf.length;
      if (size > maxBodyBytes) {
        reject(new Error("request body too large"));
        req.destroy();
        return;
      }
      chunks.push(buf);
    });
    req.on("end", () => resolveBody(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

/** @type {(argv: string[]) => { socket: string, stub: boolean }} */
function parseArgs(argv) {
  const args = { socket: "", stub: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--socket") {
      args.socket = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "--stub") {
      args.stub = true;
    }
  }
  return args;
}

/** @type {(err: unknown) => boolean} */
function isENOENT(err) {
  return typeof err === "object" && err !== null && "code" in err && err.code === "ENOENT";
}

/** @type {(socketPath: string, fetchers?: Partial<FetcherSet>) => Promise<import("node:http").Server>} */
export async function listenUnix(socketPath, fetchers = {}) {
  if (!socketPath) {
    throw new Error("unix socket path is required");
  }
  try {
    unlinkSync(socketPath);
  } catch (err) {
    if (!isENOENT(err)) {
      throw err;
    }
  }
  const server = createHelperServer(fetchers);
  await new Promise((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(socketPath, () => resolveListen(undefined));
  });
  return server;
}

const isMain = process.argv[1] != null && import.meta.url === pathToFileURL(resolve(process.argv[1])).href;
if (isMain) {
  const args = parseArgs(process.argv.slice(2));
  const server = await listenUnix(args.socket, args.stub ? stubFetchers : {});
  const shutdown = () => {
    server.close(() => process.exit(0));
  };
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
  process.stdout.write(`READY ${args.socket}\n`);
}

export { emptyCommunityPage };
