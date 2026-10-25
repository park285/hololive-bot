import { chmodSync } from "node:fs";
import { createServer } from "node:http";
import { getHeapStatistics } from "node:v8";

const args = parseArgs(process.argv.slice(2));
if (args.mode === "exit-now") {
  process.exit(1);
}
// collector가 준 V8 old space 상한이 실제 heap 한도에 반영되지 않으면 기동하지 않습니다.
// 192MiB old space에 young 영역 예약을 더한 값보다 크면 기본(cgroup 기반) 한도로 본 것입니다.
if (args.mode === "require-heap-limit" && getHeapStatistics().heap_size_limit > 320 * 1024 * 1024) {
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
      writeJSON(res, 503, healthBody("UNCONFIGURED", 0));
      return;
    }
    writeJSON(res, 200, healthBody("READY", 4));
    return;
  }
  if (req.method === "POST" && req.url === "/v1/bootstrap") {
    if (args.mode === "reject-bootstrap") {
      consume(req, () => {
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

function healthBody(state, maxInflight) {
  return {
    protocol_version: 1,
    state,
    inflight: 0,
    max_inflight: maxInflight,
    ...(args.mode === "proof-unavailable" ? { proof: unavailableProof } : {}),
  };
}

// 발급이 계속 실패하는 helper의 /health proof 진단입니다.
const unavailableProof = {
  state: "UNAVAILABLE",
  last_error: "challenge_structure",
  next_attempt_at: "2099-01-01T00:00:00.000Z",
  bootstrap_attempts: 3,
  bootstrap_successes: 1,
  upstream_requests: 4,
  minted_total: 2,
  attached_total: 5,
};

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
