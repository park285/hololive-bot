import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { once } from "node:events";
import { createRequire } from "node:module";
import { Fixture, root, docker, request, poll, generation } from "./lib/admin-bigbang-fixture.mjs";
import { attachDataFixture, dataFixtureHash } from "./lib/admin-bigbang-data-fixture.mjs";
import { SystemStats as validateStats } from "../../admin-dashboard/frontend/src/api/generated/validators/system-stats.mjs";

const require = createRequire(path.join(root, "admin-dashboard/frontend/package.json"));
const { ws: WebSocket } = require("playwright-core/lib/utilsBundle");
assert(process.argv.length === 3 || process.argv.length === 4 && process.argv[3] === "--quick", "usage: <native-candidate-image> [--quick]");
const quick = process.argv[3] === "--quick";
const durationSeconds = quick ? 600 : 3600, windowSeconds = quick ? 60 : 300;
const churnStartsSeconds = quick ? 60 : 300, churnPeriodSeconds = quick ? 4 : 30;
const f = new Fixture(process.argv[2]), sockets = new Set();
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const percentile = (values, fraction) => values.toSorted((a,b) => a-b)[Math.ceil(values.length * fraction) - 1];
const report = { schema_version: 1, status: "RUNNING", fixture_run_id: f.id, node: process.version, diagnostic_only: quick, full_gate: "NOT RUN", image: f.image, generation, corpus_sha256: dataFixtureHash,
  conditions: { duration_seconds: durationSeconds, cadence_seconds: 5, active_families: 4, streams_per_family: 4, reconnect_cycles: 100, churn_starts_seconds: churnStartsSeconds, churn_period_seconds: churnPeriodSeconds, baseline_window: `first ${windowSeconds} measurement seconds at 16 streams${quick ? " after warmup" : ""}`, recovery_window: `last ${windowSeconds} measurement seconds at 16 streams after 100 cycles`, recovery_criterion: "final-window median <= initial-window p95 for RSS, FD and goroutines", warmup: quick ? { history_seconds: 60, minimum_history_frames: 30, family_heartbeats: 4, reconnect_cycles: 16, included_in_measurement: false } : null, subscriptions: "active validated WS peers; final zero is corroborated by stopped subscriber-driven health polling and focused internal ownership tests" }, samples: [], cycles: [], errors: [], rejected_limits: [] };
const destination = path.join(root, `admin-dashboard/frontend/node_modules/.cache/admin-resource-soak${quick ? "-quick" : ""}.json`);
async function save() { await fs.writeFile(destination, JSON.stringify(report, null, 2) + "\n"); }

async function connect(auth) {
  const peer = new WebSocket(f.bffURL.replace("http:", "ws:") + "/admin/api/ws/system-stats", [`admin-stats.${generation}`], { headers: auth, handshakeTimeout: 5000 });
  sockets.add(peer);
  peer.on("error", error => report.errors.push({ reason: error.code ?? "websocket_error" }));
  peer.on("close", () => sockets.delete(peer));
  peer.frames = 0;
  peer.on("message", bytes => {
    try {
      const value = JSON.parse(bytes.toString());
      if (!validateStats(value)) throw new Error("invalid_frame");
      peer.snapshot = value; peer.receivedAt = Date.now(); peer.frames++;
    } catch { report.errors.push({ reason: "invalid_frame" }); }
  });
  await once(peer, "open"); assert.equal(peer.protocol, `admin-stats.${generation}`);
  return peer;
}
async function closePeer(peer) {
  const closed = once(peer, "close"); peer.close(1000, "fixture completed");
  await Promise.race([closed, delay(5000).then(() => { if (peer.readyState !== WebSocket.CLOSED) throw new Error("peer_close_timeout"); })]);
}
async function expectLimit(auth, kind) {
  const status = await new Promise((resolve, reject) => {
    const peer = new WebSocket(f.bffURL.replace("http:", "ws:") + "/admin/api/ws/system-stats", [`admin-stats.${generation}`], { headers: auth, handshakeTimeout: 5000 });
    peer.on("error", () => {});
    peer.on("open", () => { peer.terminate(); reject(new Error("stream limit bypassed")); });
    peer.on("unexpected-response", (_request, response) => { response.resume(); resolve(response.statusCode); peer.terminate(); });
    peer.on("close", () => reject(new Error("limit response not observed")));
  });
  assert.equal(status, 429); report.rejected_limits.push({ kind, status });
}
async function heartbeat(auth) {
  const reply = await request(f.bffURL, "POST", "/admin/api/auth/heartbeat", auth, { idle: false });
  assert.equal(reply.status, 200, "heartbeat must sustain the same family");
  const body = JSON.parse(reply.body); assert.equal(body.status, "ok");
  if (reply.headers["set-cookie"]) {
    const cookies = new Map(auth.Cookie.split("; ").map(part => [part.slice(0, part.indexOf("=")), part]));
    for (const value of reply.headers["set-cookie"]) { const part = value.split(";")[0]; cookies.set(part.slice(0, part.indexOf("=")), part); }
    auth.Cookie = [...cookies.values()].join("; ");
  }
  if (body.rotated === true) { assert.equal(typeof body.csrf_token, "string"); auth["X-CSRF-Token"] = body.csrf_token; }
  assert.equal((await request(f.bffURL, "GET", "/admin/api/auth/session", auth)).status, 200);
}

async function sampleResources(pid, peers) {
  assert.equal(sockets.size, 16); assert(peers.every(({ peer }) => peer.readyState === WebSocket.OPEN));
  const latest = peers.map(({ peer }) => peer).toSorted((a,b) => b.receivedAt - a.receivedAt)[0];
  assert(Date.now() - latest.receivedAt <= 5000, "statistics samples stopped");
  const goroutines = latest.snapshot.serviceRuntime.find(service => service.name === "admin-dashboard");
  assert(goroutines?.available && Number.isInteger(goroutines.count) && goroutines.count > 0);
  const status = await fs.readFile(`/proc/${pid}/status`, "utf8"), match = /^VmRSS:\s+(\d+) kB$/m.exec(status); assert(match);
  const fd = (await fs.readdir(`/proc/${pid}/fd`)).length;
  assert.equal(report.errors.length, 0); assert.equal(f.effects.length, 0);
  return { rss_bytes: Number(match[1]) * 1024, fd, goroutines: goroutines.count, active_validated_streams: sockets.size, frame_sampled_at: latest.snapshot.sampledAt };
}

async function warmUp(pid, peers, families) {
  const started = Date.now();
  const trace = report.warmup = { status: "RUNNING", started_at: new Date(started).toISOString(), samples: [], family_heartbeats: 0, reconnect_cycles: [] };
  const sample = async stage => {
    trace.samples.push({ stage, elapsed_ms: Date.now() - started, ...await sampleResources(pid, peers) });
    await save();
  };
  console.log("WARMUP: fill 30-frame history for 60 seconds, heartbeat four families, reconnect each of 16 peers once");
  // 이력 채움과 최초 재전송 비용은 승인한 준비 구간에 기록하고 측정 표본과 분리합니다.
  for (let index = 0; index <= 12; index++) {
    while (Date.now() - started < index * 5000) await delay(Math.max(1, started + index * 5000 - Date.now()));
    await sample("history");
  }
  await poll(() => peers.every(({ peer }) => peer.frames >= 30), "complete warmup history");
  for (const auth of families) { await heartbeat(auth); trace.family_heartbeats++; }
  await sample("heartbeat");
  for (let slot = 0; slot < peers.length; slot++) {
    const before = peers[slot];
    await closePeer(before.peer); await delay(50);
    const replacement = await connect(before.auth); peers[slot] = { auth: before.auth, peer: replacement };
    await poll(() => replacement.frames >= 30, "warmup history replay");
    trace.reconnect_cycles.push({ cycle: slot + 1, elapsed_ms: Date.now() - started, validated_frames: replacement.frames, active_peers: sockets.size });
    await sample("reconnect");
  }
  trace.elapsed_ms = Date.now() - started;
  trace.status = "COMPLETE";
  await save();
  console.log(`WARMUP COMPLETE: ${trace.elapsed_ms} ms; measured 100-cycle run starts now`);
}

try {
  attachDataFixture(f); await f.start(); assert.equal(f.architecture, "amd64");
  const families = [];
  for (let i = 0; i < 4; i++) families.push(await f.login());
  const peers = [];
  for (const auth of families) {
    for (let i = 0; i < 4; i++) peers.push({ auth, peer: await connect(auth) });
    if (peers.length === 4) await expectLimit(auth, "per-family fifth stream");
  }
  await expectLimit(await f.login(), "process seventeenth stream from a fifth family");
  await poll(() => peers.every(({ peer }) => peer.frames > 0), "initial validated statistics");
  const pid = Number(await docker(["inspect", f.bff, "--format", '{{.State.Pid}}'])); assert(Number.isSafeInteger(pid) && pid > 1);
  if (quick) await warmUp(pid, peers, families);
  const started = Date.now(); report.started_at = new Date(started).toISOString();
  console.log(`SOAK START: ${durationSeconds / 60} minutes${quick ? " diagnostic" : ""}, 16 streams, 100 reconnect cycles; no business mutations`);
  let cycle = 0;
  for (let sample = 0; sample < durationSeconds / 5; sample++) {
    await delay(Math.max(0, started + sample * 5000 - Date.now()));
    if (sample > 0 && sample % 60 === 0) for (const auth of families) await heartbeat(auth);
    // 짧은 진단의 4초 재연결 주기도 5초 표본 사이에 빠짐없이 실행합니다.
    while (cycle < 100 && Date.now() - started >= (churnStartsSeconds + cycle * churnPeriodSeconds) * 1000) {
      const slot = cycle % peers.length, before = peers[slot];
      await closePeer(before.peer); await delay(50);
      const replacement = await connect(before.auth); peers[slot] = { auth: before.auth, peer: replacement };
      await poll(() => replacement.frames > 0, "replacement validated statistics");
      report.cycles.push({ cycle: ++cycle, elapsed_ms: Date.now() - started, active_peers: sockets.size });
    }
    report.samples.push({ elapsed_ms: Date.now() - started, ...await sampleResources(pid, peers), cumulative_cycles: cycle });
    if (sample % 12 === 0) { await save(); const latest = report.samples.at(-1); console.log(`SOAK ${Math.floor(sample/12)}/${durationSeconds / 60} min: RSS=${latest.rss_bytes}, FD=${latest.fd}, goroutines=${latest.goroutines}, cycles=${cycle}`); }
  }
  // Timer가 요청보다 1ms 일찍 깨어난 실측이 있어 최소 관찰 시간은 다시 확인합니다.
  while (Date.now() - started < durationSeconds * 1000) {
    await delay(Math.max(1, started + durationSeconds * 1000 - Date.now()));
  }
  report.elapsed_ms = Date.now() - started; assert.equal(cycle, 100);
  report.recovery = {};
  const initial = report.samples.slice(0, windowSeconds / 5), final = report.samples.slice(-windowSeconds / 5);
  assert(initial.every(sample => sample.cumulative_cycles === 0));
  assert(final.every(sample => sample.cumulative_cycles === 100));
  for (const metric of ["rss_bytes", "fd", "goroutines"]) {
    const reference = percentile(initial.map(sample => sample[metric]), .95), recovered = percentile(final.map(sample => sample[metric]), .50);
    report.recovery[metric] = { initial_p95: reference, final_median: recovered, status: recovered <= reference ? "PASS" : "FAIL" };
  }
  for (const { peer } of peers) await closePeer(peer);
  assert.equal(sockets.size, 0);
  await delay(5000);
  const healthCount = f.holoReads.get("GET /health") ?? 0;
  await delay(15000);
  assert.equal(f.holoReads.get("GET /health") ?? 0, healthCount, "zero subscribers must stop upstream health polling");
  report.after_close = { active_streams: 0, subscriber_driven_health_calls_in_15_seconds: 0 };
  assert.equal(await docker(["inspect", f.bff, "--format", '{{.State.OOMKilled}}']), "false");
  const passed = Object.values(report.recovery).every(metric => metric.status === "PASS");
  report.status = passed ? quick ? "PASS_DIAGNOSTIC" : "PASS" : "FAIL";
  if (!quick) report.full_gate = report.status;
  if (!passed) process.exitCode = 1;
  console.log(`RESOURCE SOAK ${report.status}: evidence=${destination}`);
} catch (error) { report.status = "FAIL"; if (!quick) report.full_gate = "FAIL"; report.errors.push({ reason: error.message }); throw error; }
finally { await save(); for (const peer of sockets) peer.terminate(); await f.cleanup(); }
