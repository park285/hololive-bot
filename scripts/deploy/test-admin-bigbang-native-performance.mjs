import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { createLoadClients } from "./lib/admin-bigbang-load-clients.mjs";
import { Fixture, root, request, poll, docker } from "./lib/admin-bigbang-fixture.mjs";
import { attachDataFixture, dataFixtureHash } from "./lib/admin-bigbang-data-fixture.mjs";
import { loadOperationValidators } from "../../admin-dashboard/frontend/src/api/generated/validation.ts";

assert(process.argv.length === 4 || process.argv.length === 5 && process.argv[4] === "--quick", "usage: <native-baseline-image> <native-candidate-image> [--quick]");
const [baselineImage, candidateImage] = process.argv.slice(2, 4);
const quick = process.argv[4] === "--quick", cycles = quick ? 1 : 3;
const warmupMilliseconds = quick ? 5000 : 60000, measuredMilliseconds = quick ? 30000 : 120000;
const f = new Fixture(candidateImage);
const specs = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/frontend/src/api/generated/operation-contracts.json")));
const routes = [
  ["members", "/admin/api/holo/members", 1000], ["rooms", "/admin/api/holo/rooms", 100],
  ["alarms", "/admin/api/holo/alarms", 2000], ["settings", "/admin/api/holo/settings", null],
  ["streams", "/admin/api/holo/streams/live", 50], ["streams", "/admin/api/holo/streams/upcoming", 100],
  ["containers", "/admin/api/docker/containers", 9],
];
const median = values => { const sorted = values.toSorted((a, b) => a - b), mid = Math.floor(sorted.length / 2); assert(sorted.length); return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2; };
const percentile = (values, fraction) => values.toSorted((a, b) => a - b)[Math.ceil(values.length * fraction) - 1];
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const utf8 = new TextDecoder("utf-8", { fatal: true });
const report = { schema_version: 1, status: "RUNNING", fixture_run_id: f.id, diagnostic_only: quick, full_gate: "NOT RUN", required_runs: cycles * 4, baseline_image: baselineImage, candidate_image: candidateImage,
  host: { architecture: os.arch(), kernel: os.release(), cpu: os.cpus()[0].model, node: process.version }, corpus_sha256: dataFixtureHash,
  conditions: { cpus: 2, memory_bytes: 128 * 1024 ** 2, gomaxprocs: 2, gogc: 100, gomemlimit: "96MiB", warmup_seconds: warmupMilliseconds / 1000, minimum_sample_seconds: measuredMilliseconds / 1000, minimum_samples_per_route: 2000, concurrency: 8, fake_delay_ms: 10, transport: "direct fixture HTTP, one persistent keep-alive agent per client, no retries", response_validation: "every response; each of 8 independent clients owns network and validation and waits before its next GET", response_timing: "response end before body assembly; fake server and other clients do not share the receiving event loop", mutation_requests: 0 }, runs: [], errors: [] };
const destination = path.join(root, `admin-dashboard/frontend/node_modules/.cache/admin-native-performance${quick ? "-quick" : ""}.json`);
async function save() { await fs.writeFile(destination, JSON.stringify(report, null, 2) + "\n"); }

function get(base, target, headers, agent) {
  const address = new URL(base);
  return new Promise((resolve, reject) => {
    const call = http.get({ hostname: address.hostname, port: address.port, path: target, headers, agent, timeout: 10000 }, response => {
      const chunks = []; let size = 0;
      response.on("data", chunk => { size += chunk.length; if (size > 8 * 1024 ** 2) response.destroy(new Error("body_limit")); else chunks.push(chunk); });
      response.on("error", reject);
      response.on("end", () => {
        const receivedAt = performance.now(), bytes = new Uint8Array(size);
        let offset = 0;
        for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
        resolve({ status: response.statusCode, bytes, receivedAt, assemblyMilliseconds: performance.now() - receivedAt });
      });
    });
    call.on("error", reject); call.on("timeout", () => call.destroy(new Error("request_timeout")));
  });
}

async function sampleRun(variant, cycle, runNumber) {
  const image = variant === "baseline" ? baselineImage : candidateImage;
  const bff = await f.container(`performance-${runNumber}`, ["--read-only", "--tmpfs", "/tmp:rw,size=16m", "--cpus", "2", "--memory", "128m", "--pids-limit", "128", "--group-add", "1002", "--env-file", path.join(f.directory, "bff.env"), "--mount", `type=bind,src=${f.secretDirectory},dst=/fixture,readonly`, image]);
  const base = await f.url(bff, "30190/tcp");
  const agent = new http.Agent({ keepAlive: true, maxSockets: 8, maxFreeSockets: 8 });
  const result = { variant, cycle, image, started_at: new Date().toISOString(), cold: [], warmup: {}, samples: {}, rss: [], errors: [] };
  report.runs.push(result); await save();
  let loadClients;
  try {
    await poll(async () => (await request(base, "GET", "/health").catch(() => ({ status: 0 }))).status === 200, "native performance BFF");
    const headers = await f.login(base), expected = [];
    for (const [key, route, count] of routes) {
      const start = performance.now(), response = await get(base, route, headers, agent);
      result.cold.push({ route, status: response.status, milliseconds: response.receivedAt - start, bytes: response.bytes.length });
      assert.equal(response.status, 200, route);
      const body = JSON.parse(utf8.decode(response.bytes));
      assert.equal(body.status, "ok", route);
      if (count !== null) assert.equal(body[key]?.length, count, route);
      else assert.equal(body.settings.alarmAdvanceMinutes, 15);
      if (variant === "candidate") {
        const operation = specs.find(operation => operation.method === "GET" && operation.path === route);
        const validators = await loadOperationValidators(operation.id);
        const generation = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/frontend/src/api/generated/provenance.json"))).generation;
        assert(validators[operation.responses["200"].validator]({ headers: { "x-admin-server-generation": generation }, body }), route);
        if (key === "members") assert.equal(body.members[0].id, "9007199254740993");
      }
      expected.push(body);
    }
    agent.destroy();
    loadClients = await createLoadClients({ base, routes: routes.map(([, route]) => route), headers, expected });
    const pid = Number(await docker(["inspect", bff, "--format", '{{.State.Pid}}'])); assert(Number.isSafeInteger(pid) && pid > 1);
    async function rss() {
      const status = await fs.readFile(`/proc/${pid}/status`, "utf8");
      const match = /^VmRSS:\s+(\d+) kB$/m.exec(status); assert(match, "live process RSS required");
      return Number(match[1]) * 1024;
    }
    async function load(milliseconds, measured) {
      const start = performance.now(); let active = true, samplerError;
      const sampler = measured ? (async () => { while (active) { result.rss.push({ milliseconds: performance.now() - start, bytes: await rss() }); await delay(1000); } })().catch(error => { samplerError = error; loadClients.fail(error); }) : Promise.resolve();
      try {
        const phase = await loadClients.phase(milliseconds, measured ? 2000 : 0);
        result.errors.push(...phase.errors.map(error => ({ ...error, phase: measured ? "measured" : "warmup" })));
        return phase;
      } catch (error) {
        result.sampling_failure = { phase: measured ? "measured" : "warmup", reason: error.message, completed_counts: error.completedCounts };
        throw error;
      } finally {
        active = false; await sampler;
        if (samplerError) {
          result.sampling_failure = { phase: "measured", reason: samplerError.message };
          throw samplerError;
        }
      }
    }
    console.log(`WARMUP ${runNumber}/${cycles * 4}: ${variant} cycle=${cycle}`);
    const warmup = await load(warmupMilliseconds, false); result.warmup = { elapsed_seconds: warmup.elapsed_seconds, counts: warmup.counts, client_seconds: warmup.client_seconds };
    assert.equal(result.errors.length, 0, "warmup errors must not be discarded");
    console.log(`MEASURE ${runNumber}/${cycles * 4}: ${variant}`);
    const measured = await load(measuredMilliseconds, true);
    result.measured_seconds = measured.elapsed_seconds;
    result.client_seconds = measured.client_seconds;
    for (const [index, [, route]] of routes.entries()) result.samples[route] = { count: measured.counts[index], p95_ms: percentile(measured.latencies[index], .95), latencies_ms: measured.latencies[index] };
    result.rss_median_bytes = median(result.rss.map(sample => sample.bytes));
    assert.equal(await docker(["inspect", bff, "--format", '{{.State.OOMKilled}}']), "false");
    assert.equal(f.effects.length, 0); assert.equal(result.errors.length, 0);
    result.status = "PASS_SAMPLE";
    console.log(`SAMPLE ${runNumber}/${cycles * 4}: ${variant} requests=${measured.counts.reduce((a,b) => a+b)} RSS=${result.rss_median_bytes}`);
  } finally {
    agent.destroy();
    await loadClients?.close();
    await docker(["stop", "--time", "25", bff]);
    result.exit_code = Number(await docker(["inspect", bff, "--format", '{{.State.ExitCode}}']));
    await save();
    assert.equal(result.exit_code, 0);
  }
}

try {
  for (const image of [baselineImage, candidateImage]) {
    assert.equal(await docker(["image", "inspect", image, "--format", '{{.Architecture}}']), "amd64");
    assert.equal(await docker(["image", "inspect", image, "--format", '{{index .Config.Labels "dev.iris.local-fixture"}}']), "true");
  }
  attachDataFixture(f); await f.start();
  await docker(["stop", "--time", "25", f.bff]);
  let number = 0;
  for (let cycle = 1; cycle <= cycles; cycle++) for (const variant of ["baseline", "candidate", "candidate", "baseline"]) await sampleRun(variant, cycle, ++number);
  const baseline = report.runs.filter(run => run.variant === "baseline"), candidate = report.runs.filter(run => run.variant === "candidate");
  report.comparison = { routes: {}, rss: { baseline: median(baseline.map(run => run.rss_median_bytes)), candidate: median(candidate.map(run => run.rss_median_bytes)), budget_ratio: 1.10 } };
  for (const [, route] of routes) {
    const old = median(baseline.map(run => run.samples[route].p95_ms)), next = median(candidate.map(run => run.samples[route].p95_ms));
    report.comparison.routes[route] = { baseline_p95_ms: old, candidate_p95_ms: next, ratio: next / old, budget_ratio: 1.15, status: next <= old * 1.15 ? "PASS" : "FAIL" };
  }
  const memory = report.comparison.rss; memory.ratio = memory.candidate / memory.baseline; memory.status = memory.ratio <= memory.budget_ratio ? "PASS" : "FAIL";
  const passed = memory.status === "PASS" && Object.values(report.comparison.routes).every(route => route.status === "PASS");
  report.status = passed ? quick ? "PASS_DIAGNOSTIC" : "PASS" : "FAIL";
  if (!quick) report.full_gate = report.status;
  if (!passed) process.exitCode = 1;
  console.log(`NATIVE PERFORMANCE ${report.status}: evidence=${destination}`);
} catch (error) { report.status = "FAIL"; if (!quick) report.full_gate = "FAIL"; report.errors.push(error.message); throw error; }
finally { await save(); await f.cleanup(); }
