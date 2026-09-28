import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "node:http";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ProofBrokerClient } from "./proof-broker.mjs";

/** @param {(server: import("node:http").Server) => void} [configure] */
async function brokerServer(t, handler, configure = () => {}) {
  const directory = await mkdtemp(join(tmpdir(), "proof-client-"));
  const socket = join(directory, "worker.sock");
  const server = createServer(handler);
  configure(server);
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(socket, resolve);
  });
  const client = new ProofBrokerClient({ socket });
  t.after(async () => {
    client.close();
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
    await rm(directory, { recursive: true });
  });
  return client;
}

test("escaped aggregate challenge exceeding the IPC budget is rejected before admission", async (t) => {
  let admitted = 0;
  const client = await brokerServer(t, (req, res) => {
    admitted += 1;
    req.resume();
    res.end("{}");
  });
  const independentlyBounded = "\\".repeat(300_000);
  await assert.rejects(() => client.challenge({
    generation: "generation", program: independentlyBounded,
    global_name: "global", interpreter: independentlyBounded,
  }, new AbortController().signal), { code: "broker_request_size" });
  assert.equal(admitted, 0);
});


test("malformed preparation acknowledgment is not accepted", async (t) => {
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ protocol_version: 1, generation: "generation", prepared: false }));
  });
  await assert.rejects(() => client.prepare("generation", "agent", new AbortController().signal), { code: "broker_protocol" });
});

test("prepared health phase is recognized and retired before a new generation", async (t) => {
  let retired = false;
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    res.setHeader("content-type", "application/json");
    if (req.url === "/health") {
      res.end(JSON.stringify({ protocol_version: 1, generation: retired ? "new-generation" : "old-generation",
        state: retired ? "IDLE" : "AWAITING_CHALLENGE", revision: "revision" }));
    } else {
      retired = true;
      res.end(JSON.stringify({ protocol_version: 1, generation: "old-generation", closed: true }));
    }
  });
  assert.equal(await client.freshGeneration(new AbortController().signal), "new-generation");
  assert.equal(retired, true);
});

test("generation acquisition waits for trusted SDK startup beyond three seconds", async (t) => {
  let timer;
  t.after(() => clearTimeout(timer));
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    timer = setTimeout(() => {
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ protocol_version: 1, generation: "loaded-generation", state: "IDLE", revision: "revision" }));
    }, 4_000);
  });
  assert.equal(await client.freshGeneration(new AbortController().signal), "loaded-generation");
});

test("generation acquisition stops when worker readiness exhausts its bound", { timeout: 10_000 }, async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  t.mock.method(AbortSignal, "timeout", (ms) => {
    const controller = new AbortController();
    setTimeout(() => controller.abort(new DOMException("timed out", "TimeoutError")), ms);
    return controller.signal;
  });
  const entered = Promise.withResolvers();
  const stopped = new AbortController();
  t.after(() => stopped.abort());
  const client = await brokerServer(t, (req) => { req.resume(); entered.resolve(); });
  const rejected = assert.rejects(client.freshGeneration(stopped.signal), { code: "broker_unavailable" });
  await entered.promise;
  t.mock.timers.tick(40_000);
  await new Promise((resolve) => setImmediate(resolve));
  t.mock.timers.tick(50);
  await rejected;
});

for (const [name, mutation] of [
  ["another generation", { generation: "new-generation" }],
  ["another video", { video_id: "different-video" }],
  ["unexpected fields", { debug: "untrusted-data" }],
]) {
  test(`mint output from ${name} cannot be consumed`, async (t) => {
    const client = await brokerServer(t, (req, res) => {
      req.resume();
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({
        protocol_version: 1, generation: "expected-generation", video_id: "expected-video",
        po_token: "valid-proof", ...mutation,
      }));
    });
    await assert.rejects(
      () => client.mint("expected-generation", "expected-video", new AbortController().signal),
      { code: "broker_protocol" },
    );
  });
}

test("oversized untrusted output is rejected before token consumption", async (t) => {
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({
      protocol_version: 1, generation: "generation", video_id: "video", po_token: "x".repeat(70_000),
    }));
  });
  await assert.rejects(() => client.mint("generation", "video", new AbortController().signal), { code: "broker_protocol" });
});

test("closing the owned transport terminates an unfinished mint", async (t) => {
  const admitted = Promise.withResolvers();
  const client = await brokerServer(t, (req) => {
    req.resume();
    admitted.resolve();
  });
  const pending = client.mint("generation", "video", new AbortController().signal);
  const rejected = assert.rejects(pending, { code: "broker_unavailable" });
  await admitted.promise;
  client.close();
  await rejected;
});

/** @param {import("node:http").ServerResponse} res */
function healthResponse(res) {
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify({ protocol_version: 1, generation: "generation", state: "IDLE", revision: "revision" }));
}

// po-broker(Go net/http)는 Keep-Alive timeout 힌트를 보내지 않고 IdleTimeout 2초 뒤에야 닫습니다.
// 서버 쪽 idle 종료를 끄면 제한 시간 안의 종료는 모두 client가 시작한 것입니다.
const brokerIdleTimeoutMs = 2_000;

test("sequential exchanges share one connection that the client closes before the broker idle timeout", async (t) => {
  /** @type {{ clientEnded: boolean, closed: Promise<number> }[]} */
  const connections = [];
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    healthResponse(res);
  }, (server) => {
    server.keepAliveTimeout = 0;
    server.on("connection", (socket) => {
      const connection = { clientEnded: false, closed: new Promise((resolve) => socket.once("close", () => resolve(performance.now()))) };
      socket.once("end", () => { connection.clientEnded = true; });
      connections.push(connection);
    });
  });
  const signal = new AbortController().signal;
  for (let i = 0; i < 3; i += 1) await client.exchange("GET", "/health", undefined, signal);
  const idleSince = performance.now();
  assert.equal(connections.length, 1);

  const closedAt = await Promise.race([
    connections[0].closed,
    new Promise((resolve) => setTimeout(resolve, brokerIdleTimeoutMs, undefined)),
  ]);
  assert.ok(typeof closedAt === "number" && closedAt - idleSince < brokerIdleTimeoutMs, "idle socket outlived the broker idle timeout");
  assert.equal(connections[0].clientEnded, true);
});

test("an exchange slower than the idle socket timeout still completes", async (t) => {
  /** @type {NodeJS.Timeout | undefined} */
  let timer;
  t.after(() => clearTimeout(timer));
  const client = await brokerServer(t, (req, res) => {
    req.resume();
    timer = setTimeout(() => healthResponse(res), 1_500);
  }, (server) => { server.keepAliveTimeout = 0; });
  const health = await client.exchange("GET", "/health", undefined, new AbortController().signal);
  assert.equal(health.generation, "generation");
});
