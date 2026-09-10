import assert from "node:assert/strict";
import { createRequire } from "node:module";
import fs from "node:fs/promises";
import path from "node:path";
import { Fixture, root, syntheticPassword, request, poll, docker } from "./lib/admin-bigbang-fixture.mjs";
import { BrowserOrigin } from "./lib/admin-bigbang-browser-origin.mjs";
import { scriptTransfers } from "./lib/admin-js-transfer-metrics.mjs";
import { attachDataFixture, dataFixtureHash } from "./lib/admin-bigbang-data-fixture.mjs";

const require = createRequire(path.join(root, "admin-dashboard/frontend/package.json"));
const { chromium, expect } = require("@playwright/test");
assert.equal(process.argv.length, 4, "usage: <native-baseline-image> <native-candidate-image>");
const [baselineImage, candidateImage] = process.argv.slice(2);
const f = new Fixture(candidateImage), origin = new BrowserOrigin(f);
const report = { schema_version: 1, browser: "", browser_lifetime: "fresh browser and context per sample; fixed fixture topology", corpus_sha256: dataFixtureHash, initial_path: "/login", settled_path: "/dashboard/stats", compression: "identity for both variants", metric: "CDP Network.dataReceived.encodedDataLength for scripts, including canceled transfers", samples: [], budget_ratio: 1.10 };
try {
  await origin.start(); attachDataFixture(f); await f.start();
  assert.equal(f.architecture, "amd64");
  assert.equal(await docker(["image", "inspect", baselineImage, "--format", '{{.Architecture}}']), "amd64");
  const candidateURL = f.bffURL;
  const baseline = await f.container("baseline", ["--read-only", "--tmpfs", "/tmp:rw,size=16m", "--cpus", "2", "--memory", "128m", "--pids-limit", "128", "--group-add", "1002", "--env-file", path.join(f.directory, "bff.env"), "--mount", `type=bind,src=${f.secretDirectory},dst=/fixture,readonly`, baselineImage]);
  const baselineURL = await f.url(baseline, "30190/tcp");
  await poll(async () => (await request(baselineURL, "GET", "/health").catch(() => ({ status: 0 }))).status === 200, "native baseline");
  // Chrome의 ERR_NETWORK_CHANGED를 유발하는 bridge 생성/정리는 측정 구간 밖에서만 수행합니다.
  for (let cycle = 0; cycle < 3; cycle++) for (const variant of ["baseline", "candidate", "candidate", "baseline"]) {
    console.log(`START JS: ${variant} cycle=${cycle + 1}`);
    f.bffURL = variant === "baseline" ? baselineURL : candidateURL;
    origin.scriptTransfers = []; origin.errors = [];
    let context, browser;
    try {
      browser = await chromium.launch({ executablePath: "/usr/bin/google-chrome", headless: true });
      if (!report.browser) report.browser = browser.version();
      assert.equal(browser.version(), report.browser);
      context = await browser.newContext({ ignoreHTTPSErrors: true, serviceWorkers: "block", viewport: { width: 1280, height: 900 } });
      await context.addInitScript(() => { window.__fixtureCSP = []; document.addEventListener("securitypolicyviolation", event => window.__fixtureCSP.push({ directive: event.effectiveDirective, blocked: event.blockedURI })); });
      const page = await context.newPage(), pageErrors = [], consoleErrors = [], failures = [];
      const measurement = await scriptTransfers(context, page);
      page.on("pageerror", error => pageErrors.push(error.name));
      page.on("console", message => { if (message.type() === "error") consoleErrors.push(message.text()); });
      page.on("requestfailed", request => failures.push({ method: request.method(), path: new URL(request.url()).pathname, reason: request.failure()?.errorText }));
      await page.goto(origin.url + "/login");
      try {
        await page.getByLabel("아이디", { exact: true }).fill("admin", { timeout: 10000 });
        await page.getByLabel("비밀번호", { exact: true }).fill(syntheticPassword);
        await page.getByRole("button", { name: "로그인", exact: true }).click();
        await expect(page).toHaveURL(/\/dashboard\/stats$/);
      } catch (error) {
        await page.screenshot({ path: `/tmp/hololive-admin-js-${variant}-startup.png` });
        console.log({ variant, pageErrors, consoleErrors, failures, proxy: origin.errors, csp: await page.evaluate(() => window.__fixtureCSP) });
        throw error;
      }
      await page.getByText("1,000", { exact: true }).first().waitFor({ timeout: 15000 });
      await page.waitForFunction(() => performance.getEntriesByType("resource").some(entry => /SystemStatsChart.*\.js/.test(entry.name)), undefined, { timeout: 15000 });
      await page.waitForTimeout(1000);
      assert.deepEqual(pageErrors, []); assert(!consoleErrors.some(message => message.includes("ERR_NETWORK_CHANGED")));
      const violations = await page.evaluate(() => window.__fixtureCSP); assert.deepEqual(violations, []);
      const { bytes, transfers } = measurement(); assert(bytes > 0); assert(transfers.every(transfer => transfer.encoding === "identity"));
      assert.equal(f.effects.length, 0);
      report.samples.push({ cycle, variant, image: variant === "baseline" ? baselineImage : candidateImage, bytes, transfers, proxy_transfer_diagnostics: origin.scriptTransfers, proxy_diagnostics: origin.errors, failures, csp_violations: violations.length });
      console.log(`JS: ${variant} cycle=${cycle + 1} bytes=${bytes} scripts=${transfers.length}`);
    } finally { await context?.close(); await browser?.close(); }
  }
  const median = values => { const sorted = values.toSorted((a, b) => a - b); return (sorted[2] + sorted[3]) / 2; };
  report.baseline_median = median(report.samples.filter(sample => sample.variant === "baseline").map(sample => sample.bytes));
  report.candidate_median = median(report.samples.filter(sample => sample.variant === "candidate").map(sample => sample.bytes));
  report.ratio = report.candidate_median / report.baseline_median; report.status = report.ratio <= report.budget_ratio ? "PASS" : "FAIL";
  const destination = path.join(root, "admin-dashboard/frontend/node_modules/.cache/admin-js-budget.json");
  await fs.writeFile(destination, JSON.stringify(report, null, 2) + "\n"); console.log(`JS budget: ${report.status}; ratio=${report.ratio}; evidence=${destination}`);
  if (report.status !== "PASS") process.exitCode = 1;
} finally { await origin.close(); await f.cleanup(); }
