import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "node:http";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ProofBrokerClient } from "./proof-broker.mjs";

async function brokerServer(t, handler) {
  const directory = await mkdtemp(join(tmpdir(), "proof-client-"));
  const socket = join(directory, "worker.sock");
  const server = createServer(handler);
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
