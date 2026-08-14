import { createServer } from "node:http";
import { unlinkSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { fetchCommunityFeed, createInnertube, emptyCommunityPage } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { fetchViewerFeed } from "./fetch-viewer.mjs";

const maxBodyBytes = 64 * 1024;
let proxyUrl = "";
let innertubePromise;

export function setProxyUrl(url) {
  proxyUrl = String(url ?? "").trim();
}

export async function handleJSONRequest(rawBody, required, run) {
  let payload;
  try {
    payload = JSON.parse(rawBody || "{}");
  } catch {
    return { status: 400, body: { error: "request is not JSON", error_code: "collection_failed" } };
  }
  const values = {};
  for (const field of required) {
    const value = String(payload[field] ?? "").trim();
    if (value === "") {
      return { status: 400, body: { error: `${field} is required`, error_code: "collection_failed" } };
    }
    values[field] = value;
  }
  setProxyUrl(payload.proxy_url);
  try {
    const body = await run(payload, values);
    return { status: 200, body };
  } catch (err) {
    return { status: 500, body: helperError(err) };
  }
}

export async function handleCommunityRequest(rawBody, fetchCommunity) {
  return handleJSONRequest(rawBody, ["channel_id"], async (payload, values) =>
    fetchCommunity({
      channelId: values.channel_id,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    }),
  );
}

export async function handleContentRequest(rawBody, fetchContent) {
  return handleJSONRequest(rawBody, ["channel_id", "kind"], async (payload, values) =>
    fetchContent({
      channelId: values.channel_id,
      kind: values.kind,
      maxResults: payload.max_results,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    }),
  );
}

export async function handleChannelRequest(rawBody, fetchChannel) {
  return handleJSONRequest(rawBody, ["channel_id"], async (payload, values) =>
    fetchChannel({
      channelId: values.channel_id,
      maxPages: payload.max_pages,
      maxAggregateBytes: payload.max_aggregate_bytes,
    }),
  );
}

export async function handleViewerRequest(rawBody, fetchViewer) {
  return handleJSONRequest(rawBody, ["video_id"], async (payload, values) =>
    fetchViewer({
      videoId: values.video_id,
      maxAggregateBytes: payload.max_aggregate_bytes,
    }),
  );
}

function helperError(err) {
  const code = String(err?.code || "collection_failed");
  return {
    error: String(err?.message || err),
    error_code: code,
  };
}

async function proxiedFetch(input, init = {}) {
  if (proxyUrl === "") {
    return globalThis.fetch(input, init);
  }
  const { ProxyAgent, fetch } = await import("undici");
  return fetch(input, { ...init, dispatcher: new ProxyAgent(proxyUrl) });
}

async function innertubeClient() {
  if (innertubePromise == null) {
    innertubePromise = createInnertube({ fetchImpl: proxiedFetch });
  }
  return innertubePromise;
}

async function realFetchCommunity(options) {
  const innertube = await innertubeClient();
  const { YTNodes } = await import("youtubei.js");
  return fetchCommunityFeed({
    ...options,
    innertube,
    postType: YTNodes.BackstagePost,
  });
}

async function realFetchContent(options) {
  return fetchContentFeed({ ...options, innertube: await innertubeClient() });
}

async function realFetchChannel(options) {
  return fetchChannelFeed({ ...options, innertube: await innertubeClient() });
}

async function realFetchViewer(options) {
  return fetchViewerFeed({ ...options, innertube: await innertubeClient() });
}

export function createHelperServer({
  fetchCommunity = realFetchCommunity,
  fetchContent = realFetchContent,
  fetchChannel = realFetchChannel,
  fetchViewer = realFetchViewer,
} = {}) {
  return createServer(async (req, res) => {
    try {
      if (req.method === "GET" && req.url === "/health") {
        writeJSON(res, 200, { ok: true });
        return;
      }
      if (req.method === "POST" && req.url === "/v1/community") {
        const result = await handleCommunityRequest(await readBody(req), fetchCommunity);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/content") {
        const result = await handleContentRequest(await readBody(req), fetchContent);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/channel") {
        const result = await handleChannelRequest(await readBody(req), fetchChannel);
        writeJSON(res, result.status, result.body);
        return;
      }
      if (req.method === "POST" && req.url === "/v1/viewer") {
        const result = await handleViewerRequest(await readBody(req), fetchViewer);
        writeJSON(res, result.status, result.body);
        return;
      }
      writeJSON(res, 404, { error: "not found", error_code: "collection_failed" });
    } catch (err) {
      writeJSON(res, 500, helperError(err));
    }
  });
}

function writeJSON(res, status, body) {
  const raw = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(raw),
  });
  res.end(raw);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on("data", (chunk) => {
      size += chunk.length;
      if (size > maxBodyBytes) {
        reject(new Error("request body too large"));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function parseArgs(argv) {
  const args = { socket: "", stub: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--socket") {
      args.socket = String(argv[i + 1] || "");
      i += 1;
      continue;
    }
    if (arg === "--stub") {
      args.stub = true;
    }
  }
  return args;
}

function stubCommunity() {
  return {
    posts: [],
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
}

function stubContent() {
  return {
    items: [],
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
}

function stubChannel() {
  return {
    live_sessions: [],
    stats: {},
    profile: {},
    photo: [],
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
}

function stubViewer({ videoId } = {}) {
  return {
    video_id: String(videoId ?? ""),
    viewer_count: null,
    availability: "UNAVAILABLE",
    page_count: 1,
    exhausted: true,
    continuity: "CONTIGUOUS",
  };
}

export async function listenUnix(socketPath, fetchers = {}) {
  if (!socketPath) {
    throw new Error("unix socket path is required");
  }
  try {
    unlinkSync(socketPath);
  } catch (err) {
    if (err?.code !== "ENOENT") {
      throw err;
    }
  }
  const server = createHelperServer(fetchers);
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, () => resolve());
  });
  return server;
}

const isMain = process.argv[1] != null && import.meta.url === pathToFileURL(resolve(process.argv[1])).href;
if (isMain) {
  const args = parseArgs(process.argv.slice(2));
  const fetchers = args.stub
    ? {
        fetchCommunity: stubCommunity,
        fetchContent: stubContent,
        fetchChannel: stubChannel,
        fetchViewer: stubViewer,
      }
    : {};
  const server = await listenUnix(args.socket, fetchers);
  const shutdown = () => {
    server.close(() => process.exit(0));
  };
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
  process.stdout.write(`READY ${args.socket}\n`);
}

export { emptyCommunityPage };
