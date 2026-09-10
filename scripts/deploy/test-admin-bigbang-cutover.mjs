import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { once } from "node:events";
import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { Fixture, docker, generation, poll, request, root, run } from "./lib/admin-bigbang-fixture.mjs";
import { IngressFixture } from "./lib/admin-bigbang-ingress-fixture.mjs";
import { SystemStats as validateSystemStats } from "../../admin-dashboard/frontend/src/api/generated/validators/system-stats.mjs";

const require = createRequire(path.join(root, "admin-dashboard/frontend/package.json"));
const { ws: WebSocket } = require("playwright-core/lib/utilsBundle");
assert.equal(process.argv.length, 3);
const f = new Fixture(process.argv[2]);
const evidence = { schema_version: 1, fixture_image: f.image, generation, phases: [], production_effects: 0 };
const sockets = [];
let currentFrames = 0, invalidCurrentFrames = 0;
async function stream(base, headers, current) {
  const socket = new WebSocket(base.replace("http:", "ws:") + "/admin/api/ws/system-stats", current ? [`admin-stats.${generation}`] : [], { headers });
  if (current) socket.on("message", data => {
    try { if (validateSystemStats(JSON.parse(data.toString()))) currentFrames++; else invalidCurrentFrames++; }
    catch { invalidCurrentFrames++; }
  });
  sockets.push(socket); await once(socket, "open");
  if (current) assert.equal(socket.protocol, `admin-stats.${generation}`);
  return socket;
}
async function record(name, work, budget) {
  console.log(`START: ${name}`);
  const began = Date.now(), detail = await work(), elapsed_ms = Date.now() - began;
  assert(elapsed_ms <= budget, `${name} exceeded frozen budget`); evidence.phases.push({ name, elapsed_ms, ...detail });
}
try {
  await f.start(); await docker(["stop", "--time", "25", f.bff]);
  const oldPolicy = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/docs/bigbang/old-proxy-policy.json")));
  const newPolicy = JSON.parse(await fs.readFile(path.join(root, "deploy/compose/admin-docker-policy.generated.json"))).services["admin-docker-proxy"].command;
  await f.applyProxy(oldPolicy.command);
  evidence.old_proxy_command_sha256 = oldPolicy.command_sha256;
  console.log("READY: isolated fixture; candidate stopped");
  const old = await f.startOldBFF(); evidence.old_image = old.image;
  console.log("READY: old artifact");
  const ingress = await new IngressFixture(f).start(f.oldURL);
  await poll(async () => (await request(ingress.url, "GET", "/health").catch(() => ({ status: 0 }))).status === 200, "old ingress");
  console.log("READY: old ingress");
  const originalIngressStart = await docker(["inspect", ingress.container, "--format", '{{.State.StartedAt}}']);
  const oldAuth = await f.login(ingress.url), oldSocket = await stream(ingress.url, oldAuth, false);
  console.log("READY: old login and websocket");
  await ingress.shortlink();
  f.holdDocker = true;
  let settled = false;
  const oldMutation = request(ingress.url, "POST", "/admin/api/docker/containers/hololive-api/restart", oldAuth).then(response => { settled = true; return { status: response.status }; }, () => { settled = true; return { status: "connection_lost" }; });
  await poll(() => Boolean(f.heldDockerResponse), "old upstream effect held"); assert.equal(f.effects.length, 1);
  let stopping;
  await record("ingress_fence", async () => {
    const result = await ingress.maintenance("fence");
    // C06: ingress reload 뒤 기존 client는 이전 worker를 쓸 수 있어 BFF listener 종료가 실제 admission 경계입니다.
    stopping = docker(["stop", "--time", "25", f.old]);
    await poll(async () => (await request(f.oldURL, "GET", "/health").catch(() => ({ status: 0 }))).status === 0, "old listener closed");
    const rejected = await request(ingress.url, "POST", "/admin/api/docker/containers/hololive-api/restart", oldAuth);
    assert([502, 503].includes(rejected.status)); assert.equal(f.effects.length, 1); assert.equal(settled, false);
    await ingress.shortlink(); return { result, new_upstream_effects: 0, shortlink: 302 };
  }, 30000);
  await record("old_drain_and_shutdown", async () => {
    f.heldDockerResponse.destroy(); f.holdDocker = false;
    const client = await oldMutation;
    await stopping; await poll(() => oldSocket.readyState === WebSocket.CLOSED, "old WS closed");
    assert.equal(await docker(["inspect", f.old, "--format", '{{.State.Status}}']), "exited");
    assert.equal(await docker(["inspect", f.old, "--format", '{{.State.ExitCode}}']), "0");
    const direct = await request(f.oldURL, "POST", "/admin/api/docker/containers/hololive-api/restart", oldAuth).catch(() => ({ status: 0 }));
    assert.equal(direct.status, 0); assert.equal(f.effects.length, 1);
    return { client_status: client.status, logical_outcome: "unknown", owner_effects: 1, business_replays: 0, direct_admission: "closed", ws: "closed" };
  }, 25000);
  await f.cache(["SET", "session:twentyq:fixture-preserve", "unchanged"]);
  await f.cache(["SET", "holo:settings:fixture-preserve", "unchanged"]);
  await f.cache(["HSET", "session:admin:family:purge-proof", "token", "fixture", `mutation:${randomUUID()}`, "1"]);
  await record("admin_session_purge_and_new_secret", async () => {
    const result = await ingress.purge(f.old);
    assert.equal(await f.cache(["--scan", "--pattern", "session:admin:*"]), "");
    for (const key of ["session:twentyq:fixture-preserve", "holo:settings:fixture-preserve"]) assert.equal(await f.cache(["GET", key]), "unchanged");
    const previous = f.secret; await f.materialize(); assert.notEqual(f.secret, previous);
    await f.applyProxy(newPolicy);
    await ingress.remove(f.bff); await f.startBFF();
    await ingress.target(f.bffURL); await ingress.render();
    assert.equal((await request(ingress.url, "GET", "/health")).status, 503);
    const metadata = await request(f.bffURL, "GET", "/admin/meta.json"); assert.equal(JSON.parse(metadata.body).clientGeneration, generation);
    assert.equal((await request(f.bffURL, "GET", "/admin/api/auth/session", oldAuth)).status, 401);
    return { result, non_admin_data: "preserved", secret: "new", metadata: "matched", old_cookie: "rejected" };
  }, 180000);
  await record("candidate_open_and_smoke", async () => {
    await ingress.maintenance("open"); await ingress.shortlink();
    const auth = await f.login(ingress.url), socket = await stream(ingress.url, auth, true);
    const health = await request(ingress.url, "GET", "/admin/api/docker/health", auth);
    assert.equal(JSON.parse(health.body).available, true);
    assert.equal((await request(ingress.url, "GET", "/admin/api/auth/session", { Cookie: oldAuth.Cookie })).status, 409);
    f.currentAuth = auth; f.currentSocket = socket;
    return { metadata: "matched", new_login: "PASS", websocket: "current_protocol", old_bundle_generation: "rejected", docker: "available", business_replays: 0 };
  }, 180000);
  await record("candidate_observation", async () => {
    const began = Date.now();
    for (let sample = 0; sample < 60; sample++) {
      const delay = began + sample * 5000 - Date.now();
      if (delay > 0) await new Promise(resolve => setTimeout(resolve, delay));
      assert.equal((await request(ingress.url, "GET", "/health")).status, 200);
      assert.equal((await request(ingress.url, "GET", "/admin/api/auth/session", f.currentAuth)).status, 200);
      assert.equal(f.currentSocket.readyState, WebSocket.OPEN); assert.equal(invalidCurrentFrames, 0);
      await ingress.shortlink(); assert.equal(f.effects.length, 1);
      if (sample % 12 === 0) console.log(`OBSERVE: ${sample + 1}/60; current WS frames=${currentFrames}`);
    }
    assert(currentFrames > 1);
    return { window_limit_ms: 300000, cadence_ms: 5000, samples: 60, validated_frames: currentFrames, invalid_frames: invalidCurrentFrames, business_effects: 0, shortlink: "preserved" };
  }, 300000);
  await record("whole_rollback", async () => {
    await ingress.maintenance("fence"); await docker(["stop", "--time", "25", f.bff]);
    await poll(() => f.currentSocket.readyState === WebSocket.CLOSED, "candidate WS closed");
    const result = await ingress.purge(f.bff); const previous = f.secret; await f.materialize(); assert.notEqual(f.secret, previous);
    await f.applyProxy(oldPolicy.command);
    await ingress.remove(f.old); await f.startOldBFF(); await ingress.target(f.oldURL); await ingress.render();
    for (const auth of [oldAuth, f.currentAuth]) assert.equal((await request(f.oldURL, "GET", "/admin/api/auth/session", auth)).status, 401);
    await ingress.maintenance("open"); f.rollbackAuth = await f.login(ingress.url); await ingress.shortlink();
    assert.equal(f.effects.length, 1); assert.equal(await f.cache(["GET", "session:twentyq:fixture-preserve"]), "unchanged");
    return { result, image: old.image, secret: "new_again", old_secrets_reused: 0, cookies: "previous_generations_rejected", business_replays: 0 };
  }, 300000);
  await record("running_BFF_purge_denied", async () => {
    const before = await f.cache(["DBSIZE"]);
    await assert.rejects(ingress.purge(f.old), error => error.code === 1);
    assert.equal(await f.cache(["DBSIZE"]), before);
    return { status: "denied", cache_changes: 0 };
  }, 5000);
  await record("proxy_failure_keeps_maintenance", async () => {
    await ingress.maintenance("fence"); await ingress.target(f.oldURL.replace(":30190", ":9")); await ingress.render();
    const failure = await run("bash", [path.join(ingress.directory, "scripts/deploy/admin-dashboard-maintenance.sh"), "open", ingress.container, ingress.config], { timeout: 45000 }).then(() => null, error => error);
    assert(failure && failure.code === 1); assert(failure.stderr.includes("maintenance_compensation=confirmed"));
    await poll(async () => (await request(ingress.url, "GET", "/health", { Connection: "close" })).status === 503, "compensated ingress reaches existing clients"); await ingress.shortlink();
    assert.equal(f.effects.length, 1); await ingress.target(f.oldURL); await ingress.render();
    return { outcome: "NO-GO", maintenance: "confirmed", business_replays: 0, shortlink: 302 };
  }, 45000);
  await record("old_shutdown_timeout_preserves_unknown", async () => {
    f.holdDocker = true; f.heldDockerResponse = null;
    const pending = request(f.oldURL, "POST", "/admin/api/docker/containers/hololive-api/restart", f.rollbackAuth, undefined, 60000).then(response => response.status, () => "connection_lost");
    await poll(() => Boolean(f.heldDockerResponse), "old timeout effect"); assert.equal(f.effects.length, 2);
    const began = Date.now(); await docker(["stop", "--time", "25", f.old]); const stop_ms = Date.now() - began;
    const code = await docker(["inspect", f.old, "--format", '{{.State.ExitCode}}']);
    const client = await pending; assert(stop_ms >= 19000, "held request did not exercise the old drain deadline");
    assert.equal((await request(ingress.url, "GET", "/health")).status, 503); await ingress.shortlink();
    assert.equal(f.effects.length, 2); f.heldDockerResponse.destroy(); f.holdDocker = false;
    return { outcome: "NO-GO", client_status: client, logical_outcome: "unknown", owner_effects: 1, exit_code: code, stop_ms, business_replays: 0, maintenance: "preserved" };
  }, 40000);
  assert.equal(await docker(["inspect", ingress.container, "--format", '{{.State.StartedAt}}']), originalIngressStart, "shared ingress was recreated");
  evidence.status = "PASS_LOCAL_SEQUENCE";
  evidence.architecture = f.architecture;
  evidence.remaining = ["production O02 firewall evidence", "reviewed committed release identity"];
  const destination = path.join(root, "admin-dashboard/frontend/node_modules/.cache/admin-cutover.json");
  await fs.writeFile(destination, JSON.stringify(evidence, null, 2) + "\n"); console.log(`PASS: ${evidence.phases.length} local first-cutover/whole-rollback phases; shared ingress preserved; evidence=${destination}`);
} finally { for (const socket of sockets) socket.terminate(); f.heldDockerResponse?.destroy(); await f.cleanup(); }
