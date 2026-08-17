import { chmodSync } from "node:fs";
import { createServer } from "node:http";

const args = parseArgs(process.argv.slice(2));
if (args.mode === "exit-now") {
  process.exit(1);
}
if (args.mode === "insecure") {
  process.umask(0);
} else {
  process.umask(0o077);
}
if (args.mode === "ignore-term") {
  process.on("SIGTERM", () => {});
  process.on("SIGINT", () => {});
} else if (args.mode === "exit-error-on-term") {
  const shutdown = () => process.exit(7);
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
} else {
  const shutdown = () => process.exit(0);
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
}

const server = createServer((req, res) => {
  if (req.method === "GET" && req.url === "/health") {
    if (args.mode === "reject-bootstrap") {
      writeJSON(res, 503, healthBody("UNCONFIGURED", 0, false));
      return;
    }
    writeJSON(res, 200, healthBody("READY", 4, false));
    return;
  }
  if (req.method === "POST" && req.url === "/v1/bootstrap") {
    if (args.mode === "reject-bootstrap" || args.mode === "leak-proxy") {
      consume(req, (raw) => {
        const parsed = parseJSON(raw);
        const proxyURL = isRecord(parsed.proxy) && typeof parsed.proxy.url === "string" ? parsed.proxy.url : "";
        if (args.mode === "leak-proxy") {
          writeJSON(res, 500, {
            error: `bootstrap failed for ${proxyURL}`,
            error_code: "helper_internal_invariant",
            error_class: "Error",
          });
          return;
        }
        writeJSON(res, 409, {
          error: "protocol mismatch",
          error_code: "helper_protocol_mismatch",
          error_class: "Error",
        });
      });
      return;
    }
    consume(req, (raw) => {
      const parsed = parseJSON(raw);
      const limits = isRecord(parsed.limits) ? parsed.limits : {};
      writeJSON(res, 200, {
        protocol_version: 1,
        state: "READY",
        proxy_enabled: Boolean(isRecord(parsed.proxy) && parsed.proxy.enabled),
        request_body_bytes: numberOr(limits.request_body_bytes, 65536),
        response_body_bytes: numberOr(limits.response_body_bytes, 1048576),
        max_inflight: numberOr(limits.max_inflight, 4),
      });
    });
    return;
  }
  writeJSON(res, 404, { error: "not found", error_code: "collection_failed", error_class: "Error" });
});

await new Promise((resolve, reject) => {
  server.once("error", reject);
  server.listen(args.socket, () => {
    if (args.mode !== "insecure") {
      chmodSync(args.socket, 0o700);
    }
    if ("_pipeName" in server) {
      server._pipeName = undefined;
    }
    resolve(undefined);
  });
});

function healthBody(state, maxInflight, proxyEnabled) {
  return {
    protocol_version: 1,
    state,
    inflight: 0,
    max_inflight: maxInflight,
    proxy_enabled: proxyEnabled,
  };
}

function consume(req, done) {
  /** @type {Buffer[]} */
  const chunks = [];
  req.on("data", (chunk) => {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  });
  req.on("end", () => done(Buffer.concat(chunks).toString("utf8")));
  req.on("error", () => done("{}"));
}

function parseJSON(raw) {
  try {
    return JSON.parse(raw || "{}");
  } catch {
    return {};
  }
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function numberOr(value, fallback) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function writeJSON(res, status, body) {
  const raw = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(raw),
  });
  res.end(raw);
}

function parseArgs(argv) {
  const parsed = { socket: "", mode: "ready" };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--socket") {
      parsed.socket = String(argv[i + 1] ?? "");
      i += 1;
      continue;
    }
    if (arg === "--mode") {
      parsed.mode = String(argv[i + 1] ?? "ready");
      i += 1;
    }
  }
  return parsed;
}
