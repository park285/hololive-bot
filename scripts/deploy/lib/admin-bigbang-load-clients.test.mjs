import assert from "node:assert/strict";
import http from "node:http";
import { once } from "node:events";
import test from "node:test";
import { createLoadClients } from "./admin-bigbang-load-clients.mjs";

async function fixture(t, handler, options) {
  const sockets = new Set(), server = http.createServer(handler);
  server.on("connection", socket => { sockets.add(socket); socket.on("close", () => sockets.delete(socket)); });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  const clients = await createLoadClients({ base: `http://127.0.0.1:${server.address().port}`, headers: {}, ...options });
  t.after(async () => { await clients.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); });
  return { clients, sockets };
}

test("eight clients validate every response, balance routes and retain warmed connections", { timeout: 10000 }, async t => {
  const expected = { id: "9007199254740993", name: "한글", nullable: null, enabled: false, values: [] };
  const { clients, sockets } = await fixture(t, (_request, response) => response.end(JSON.stringify(expected)), { routes: ["/one", "/two"], expected: [expected, expected] });
  const warmup = await clients.phase(40, 8);
  assert.deepEqual(warmup.errors, []);
  assert.equal(sockets.size, 8);
  const warmed = new Set(sockets);
  const measured = await clients.phase(40, 8);
  assert.deepEqual(measured.errors, []);
  assert(Math.abs(measured.counts[0] - measured.counts[1]) <= 1);
  for (let index = 0; index < 2; index++) { assert(measured.counts[index] >= 8); assert.equal(measured.latencies[index].length, measured.counts[index]); }
  assert.deepEqual(sockets, warmed);
  assert(measured.client_seconds.every(seconds => seconds >= .04));
});

test("invalid UTF-8, missing fields, extra fields and non-200 replies remain failed samples", { timeout: 10000 }, async t => {
  const replies = [Buffer.from('{"value":"\ufffd"}'), Buffer.from([0x7b,0x22,0x76,0x61,0x6c,0x75,0x65,0x22,0x3a,0x22,0xff,0x22,0x7d]), Buffer.from('{}'), Buffer.from('{"value":"\ufffd","extra":true}')];
  const routes = ["/status", "/utf8", "/missing", "/extra"];
  const { clients } = await fixture(t, (request, response) => {
    const index = routes.indexOf(request.url); response.statusCode = index === 0 ? 503 : 200; response.end(replies[index]);
  }, { routes, expected: routes.map(() => ({ value: "\ufffd" })) });
  const result = await clients.phase(20, 1);
  const failures = result.errors.filter(error => error.route !== undefined);
  assert.equal(failures.length, result.counts.reduce((sum, count) => sum + count, 0));
  for (let index = 0; index < routes.length; index++) {
    assert.equal(failures.filter(error => error.route === routes[index]).length, result.counts[index]);
    assert.equal(result.latencies[index].length, result.counts[index]);
  }
});

test("worker failure rejects the phase with completed counts and prevents later work", { timeout: 10000 }, async t => {
  const { clients } = await fixture(t, (_request, response) => response.end('{}'), { routes: ["/"], expected: [{}] });
  const pending = clients.phase(1000, 0);
  const rejected = assert.rejects(pending, error => /exited/.test(error.message) && Array.isArray(error.completedCounts));
  await clients.slots[0].worker.terminate();
  await rejected;
  await assert.rejects(clients.phase(10, 0), /exited/);
});

test("closing active clients rejects pending work and releases stalled connections", { timeout: 10000 }, async t => {
  const { clients, sockets } = await fixture(t, () => {}, { routes: ["/"], expected: [{}] });
  const pending = clients.phase(1000, 0);
  const rejected = assert.rejects(pending, /closed/);
  while (sockets.size === 0) await new Promise(resolve => setTimeout(resolve, 5));
  await clients.close(); await rejected;
  const deadline = Date.now() + 1000;
  while (sockets.size !== 0 && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 5));
  assert.equal(sockets.size, 0);
});
