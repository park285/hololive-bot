import assert from "node:assert/strict";
import { fork } from "node:child_process";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import fs from "node:fs/promises";
import path from "node:path";
import { Fixture, root, syntheticPassword, request, generation } from "./lib/admin-bigbang-fixture.mjs";
import { BrowserOrigin } from "./lib/admin-bigbang-browser-origin.mjs";
import { attachDataFixture, dataFixtureHash } from "./lib/admin-bigbang-data-fixture.mjs";

const require = createRequire(path.join(root, "admin-dashboard/frontend/package.json"));
const { chromium, firefox, webkit, expect } = require("@playwright/test");
const worker = process.argv[2] === "--browser-worker";
assert.equal(process.argv.length, worker ? 4 : 3, "usage: <native-candidate-image>");
const configuration = worker ? JSON.parse(await fs.readFile(process.argv[3])) : null;
const f = worker ? null : new Fixture(process.argv[2]);
const origin = worker ? { url: configuration.origin } : new BrowserOrigin(f);
const report = worker ? configuration.report : { schema_version: 1, run_id: f.id, candidate_image: f.image, generation, corpus_sha256: dataFixtureHash, status: "RUNNING", engines: [], cases: [] };
const destination = path.join(root, "admin-dashboard/frontend/node_modules/.cache/admin-production-browser.json");
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
const old = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/docs/bigbang/old-assets.json")));
const current = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/frontend/node_modules/.cache/admin-build-inventory.json")));
const oldDirectory = "/tmp/hololive-admin-old-artifact/dist";
const newDirectory = path.join(root, "admin-dashboard/frontend/dist");
const routeForFile = file => file === "index.html" ? "/" : "/" + file;
const types = { ".js": "application/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml", ".woff2": "font/woff2" };
async function save() {
  if (worker) await new Promise((resolve, reject) => process.send({ kind: "evidence", report }, error => error ? reject(error) : resolve()));
  else await fs.writeFile(destination, JSON.stringify(report, null, 2) + "\n");
}
let sequence = 0;
async function control(command, variant) {
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    const receive = message => { if (message.id !== id) return; process.off("message", receive); message.error ? reject(new Error(message.error)) : resolve(message.value); };
    process.on("message", receive);
    process.send({ kind: "control", id, command, variant }, error => { if (error) { process.off("message", receive); reject(error); } });
  });
}
async function target(variant) { await control("target", variant); }
function ledger() { return { business_effects: f.effects.length, holo_mutations: [...f.holoReads].filter(([key]) => !key.startsWith("GET ")).reduce((total, [, count]) => total + count, 0), websocket_rejections: origin.errors.filter(error => error.startsWith("ws_")) }; }
async function runBrowserWorker(candidateURL) {
  const assets = {};
  for (const [name, directory, inventory] of [["old", oldDirectory, old.files], ["candidate", newDirectory, current.files]]) {
    assets[name] = new Map(await Promise.all(Object.keys(inventory).map(async file => [file, { body: await fs.readFile(path.join(directory, file)), type: types[path.extname(file)] ?? "application/octet-stream" }])));
  }
  const file = path.join(f.directory, "browser-worker.json"); await fs.writeFile(file, JSON.stringify({ origin: origin.url, report }), { mode: 0o600 });
  const cache = process.env.ADMIN_TEST_LDCACHE;
  // root 파일 준비·정리는 부모가 소유하고 브라우저 실행만 사설 ldconfig namespace에 둡니다.
  const child = fork(fileURLToPath(import.meta.url), ["--browser-worker", file], { execPath: cache ? "/usr/bin/bwrap" : process.execPath,
    execArgv: cache ? ["--bind", "/", "/", "--dev", "/dev", "--ro-bind", cache, "/etc/ld.so.cache", process.execPath] : [], stdio: ["ignore", "inherit", "inherit", "ipc"] });
  await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("message", message => {
      if (message.kind === "evidence") {
        if (message.report.run_id !== f.id || message.report.candidate_image !== f.image) { reject(new Error("browser evidence owner mismatch")); child.kill(); return; }
        Object.assign(report, message.report); return;
      }
      if (message.kind !== "control") return;
      try {
        let value;
        if (message.command === "target") { assert(["old", "candidate"].includes(message.variant)); f.bffURL = message.variant === "old" ? f.oldURL : candidateURL; origin.assets = undefined; value = true; }
        else if (message.command === "assets") { assert(Object.hasOwn(assets, message.variant)); origin.assets = assets[message.variant]; value = true; }
        else { assert.equal(message.command, "state"); value = ledger(); }
        child.send({ id: message.id, value }, error => { if (error) reject(error); });
      } catch (error) { child.send({ id: message.id, error: error.message }, sendError => { if (sendError) reject(sendError); }); }
    });
    child.on("close", code => code === 0 ? resolve() : reject(new Error("browser worker failed; code=" + code)));
  });
}

async function verifyAssets(base, directory, inventory) {
  for (const [file, expected] of Object.entries(inventory)) {
    const bytes = await fs.readFile(path.join(directory, file)); assert.equal(hash(bytes), expected.sha256, file);
    const response = await request(base, "GET", routeForFile(file));
    assert.equal(response.status, 200, file); assert.equal(hash(response.bytes), expected.sha256, `served ${file}`);
  }
  return Object.keys(inventory).length;
}
async function contextFor(browser, bundle) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, serviceWorkers: "block", viewport: { width: 1280, height: 900 } });
  await context.addInitScript(() => { window.__fixtureCSP = []; document.addEventListener("securitypolicyviolation", event => window.__fixtureCSP.push({ directive: event.effectiveDirective, blocked: event.blockedURI })); });
  // media는 합성 fixture이며 외부 플랫폼에 요청을 보내지 않습니다.
  await context.route(/^https:\/\/(?:i\.ytimg\.com|yt3\.ggpht\.com|img\.youtube\.com)\//, route => route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"/>' }));
  if (bundle) {
    // 실제 HTTPS 응답으로 제공하여 route.fulfill 문서의 Chrome LNA 주소 공간 문제를 피합니다.
    await control("assets", bundle);
  }
  return context;
}
async function login(page) {
  await page.goto(origin.url + "/login");
  await page.getByLabel("아이디", { exact: true }).fill("admin");
  await page.getByLabel("비밀번호", { exact: true }).fill(syntheticPassword);
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await expect(page).toHaveURL(/\/dashboard\/stats$/);
  await page.getByText("1,000", { exact: true }).first().waitFor({ timeout: 15000 });
}
async function cookieLogin(context) {
  const response = await context.request.post(origin.url + "/admin/api/auth/login", { headers: { Origin: origin.url, "X-Admin-Client-Generation": generation }, data: { username: "admin", password: syntheticPassword } });
  assert.equal(response.status(), 200); await response.dispose();
}
async function probeWS(page, protocols) {
  return page.evaluate(protocols => new Promise(resolve => {
    const peer = new WebSocket(location.origin.replace("https:", "wss:") + "/admin/api/ws/system-stats", protocols);
    let opened = false, frames = 0, done = false;
    const finish = () => { if (!done) { done = true; clearTimeout(timer); const protocol = peer.protocol; peer.close(); resolve({ opened, frames, protocol }); } };
    const timer = setTimeout(finish, 5000);
    peer.onopen = () => { opened = true; };
    peer.onmessage = () => { frames++; finish(); };
    peer.onerror = finish; peer.onclose = finish;
  }), protocols);
}
function observe(page) {
  const calls = [], errors = [], socketErrors = []; let frames = 0;
  page.on("response", response => { const url = new URL(response.url()); if (url.origin === origin.url && url.pathname.startsWith("/admin/")) calls.push({ path: url.pathname, method: response.request().method(), status: response.status() }); });
  page.on("pageerror", error => errors.push(error.name));
  page.on("websocket", socket => { socket.on("framereceived", () => frames++); socket.on("socketerror", error => socketErrors.push(error)); });
  page.on("console", message => { if (/WebSocket.*failed/i.test(message.text())) socketErrors.push(message.text()); });
  return { calls, errors, socketErrors, frames: () => frames };
}
const business = calls => calls.filter(call => /^\/admin\/api\/(?:holo|docker)\//.test(call.path));
async function record(browser, name, work) {
  const began = Date.now(); console.log(`BROWSER START: ${browser.browserType().name()} ${name}`);
  const detail = await work(), state = await control("state"); assert.equal(state.business_effects, 0); assert.equal(state.holo_mutations, 0);
  report.cases.push({ engine: browser.browserType().name(), name, elapsed_ms: Date.now() - began, ...detail }); await save();
}

try {
  if (!worker) {
    await origin.start(); attachDataFixture(f); await f.start(); assert.equal(f.architecture, "amd64");
    const candidateURL = f.bffURL; await f.startOldBFF(); report.old_image = old.image;
    report.verified_assets = { candidate: await verifyAssets(candidateURL, newDirectory, current.files), old: await verifyAssets(f.oldURL, oldDirectory, old.files) };
    await save(); await runBrowserWorker(candidateURL);
    const state = ledger(); assert.equal(state.business_effects, 0); assert.equal(state.holo_mutations, 0);
  } else for (const type of [chromium, firefox, webkit]) {
    const browser = await type.launch({ headless: true, ...(type === chromium ? { executablePath: "/usr/bin/google-chrome" } : {}) });
    report.engines.push({ name: type.name(), version: browser.version() });
    try {
      await record(browser, "actual old bundle and old BFF control", async () => {
        await target("old"); const context = await contextFor(browser), page = await context.newPage();
        try {
          await login(page); const oldControl = await probeWS(page, []); assert(oldControl.opened && oldControl.frames > 0);
          const rawBrowser = await probeWS(page, [`admin-stats.${generation}`]);
          // Firefox는 선택된 subprotocol이 없어도 open하므로 실제 앱의 metadata·협상 검사가 필요합니다.
          assert.equal(rawBrowser.protocol, "");
          return { old_protocol_control: oldControl, raw_browser_new_protocol_on_old_BFF: rawBrowser };
        }
        finally { await context.close(); }
      });
      await record(browser, "old bundle blocked by candidate HTTP and WS", async () => {
        await target("candidate"); const context = await contextFor(browser, "old");
        try {
          await cookieLogin(context); const page = await context.newPage(), observed = observe(page); await page.goto(origin.url + "/login");
          await expect.poll(() => observed.calls.some(call => call.status === 409)).toBe(true);
          assert(business(observed.calls).every(call => call.status === 409));
          const rejected = await probeWS(page, []); assert.deepEqual(rejected, { opened: false, frames: 0, protocol: "" });
          const controlResult = await probeWS(page, [`admin-stats.${generation}`]);
          if (!controlResult.opened || controlResult.frames === 0) {
            const apiStatus = (await context.request.get(origin.url + "/admin/api/auth/session", { headers: { "X-Admin-Client-Generation": generation } })).status();
            const browserStatus = await page.evaluate(async generation => (await fetch("/admin/api/auth/session", { headers: { "X-Admin-Client-Generation": generation } })).status, generation);
            console.log({ controlResult, apiStatus, browserStatus, socketErrors: observed.socketErrors, cookies: (await context.cookies()).map(({ name, domain, path, secure, httpOnly, sameSite }) => ({ name, domain, path, secure, httpOnly, sameSite })), csp: await page.evaluate(() => window.__fixtureCSP), ledger: await control("state") });
          }
          assert(controlResult.opened && controlResult.frames > 0);
          assert.equal(controlResult.protocol, `admin-stats.${generation}`);
          return { calls: observed.calls, old_protocol: rejected, candidate_protocol_control: controlResult, business_effects: 0 };
        } finally { await context.close(); }
      });
      await record(browser, "candidate bundle blocks before old metadata compatibility", async () => {
        await target("old"); const context = await contextFor(browser, "candidate");
        try {
          await cookieLogin(context); const page = await context.newPage(), observed = observe(page); await page.goto(origin.url + "/login");
          await expect(page.getByRole("alert")).toContainText("호환성"); await expect(page.getByRole("button", { name: "새로고침", exact: true })).toBeVisible();
          assert.equal(business(observed.calls).length, 0); assert.equal(observed.frames(), 0);
          return { calls: observed.calls, business_requests: 0, applied_frames: 0 };
        } finally { await context.close(); }
      });
      for (const mode of ["html", "missing-generation", "different-generation"]) await record(browser, `candidate metadata ${mode}`, async () => {
        await target("candidate"); const context = await contextFor(browser), page = await context.newPage(), observed = observe(page);
        try {
          await page.route(origin.url + "/admin/meta.json", async route => {
            const actual = await route.fetch(), headers = { ...actual.headers() };
            delete headers["content-length"]; delete headers["content-encoding"];
            if (mode === "html") { headers["content-type"] = "text/html"; await route.fulfill({ status: 200, headers, body: "<html>fixture maintenance</html>" }); }
            else { const body = await actual.json(); if (mode === "missing-generation") delete headers["x-admin-server-generation"]; else body.clientGeneration = "0".repeat(64); await route.fulfill({ status: 200, headers, json: body }); }
          });
          await page.goto(origin.url + "/login"); await expect(page.getByRole("alert")).toBeVisible();
          await expect(page.getByLabel("비밀번호", { exact: true })).toHaveCount(0);
          assert.equal(business(observed.calls).length, 0); assert.equal(observed.frames(), 0);
          return { calls: observed.calls, business_requests: 0, applied_frames: 0 };
        } finally { await context.close(); }
      });
      await record(browser, "candidate production menus, responsive layout and CSP", async () => {
        await target("candidate"); const context = await contextFor(browser), page = await context.newPage(), observed = observe(page);
        try {
          await login(page);
          for (const menu of ["stats", "streams", "members", "calendar", "alarms", "rooms", "settings"]) {
            await page.locator(`nav a[href="/dashboard/${menu}"]:visible`).first().click(); await expect(page).toHaveURL(new RegExp(`/dashboard/${menu}$`));
            await page.waitForTimeout(300);
            await expect(page.getByRole("alert")).toHaveCount(0);
          }
          await expect(page.getByText("hololive-api", { exact: true }).first()).toBeVisible();
          await page.setViewportSize({ width: 390, height: 844 });
          for (const menu of ["stats", "streams", "members", "calendar", "alarms", "rooms", "settings"]) {
            const opener = page.getByRole("button", { name: "메뉴 열기", exact: true }); await opener.click();
            const drawer = page.getByRole("dialog", { name: "주 내비게이션" }); await drawer.locator(`a[href="/dashboard/${menu}"]`).click();
            await expect(drawer).toHaveCount(0); await expect(opener).toBeFocused(); await expect(page).toHaveURL(new RegExp(`/dashboard/${menu}$`));
            await expect.poll(() => page.locator("#app-sidebar").evaluate(element => element.getBoundingClientRect().right)).toBeLessThanOrEqual(1);
          }
          assert.deepEqual(observed.errors, []); const csp = await page.evaluate(() => window.__fixtureCSP); assert.deepEqual(csp, []);
          assert(business(observed.calls).every(call => call.method === "GET" && call.status === 200));
          // pinned Playwright screenshotter는 WebKit 동기화를 위해 inline `body {}` style을 주입합니다.
          // 앱 검증 뒤 수행하고 그 도구의 차단 사건도 별도로 남깁니다.
          await page.screenshot({ path: `/tmp/hololive-admin-production-${type.name()}-mobile.png` });
          const captureCSP = await page.evaluate(() => window.__fixtureCSP);
          assert.deepEqual(captureCSP, type === webkit ? [{ directive: "style-src-elem", blocked: "inline" }] : []);
          return { menus: 7, mobile_menus: 7, calls: observed.calls, csp_violations: 0, screenshot_tool_csp: captureCSP, page_errors: 0, business_mutations: 0 };
        } finally { await context.close(); }
      });
      await record(browser, "missing validator asset blocks the protected read", async () => {
        await target("candidate"); const context = await contextFor(browser), page = await context.newPage(), observed = observe(page);
        try {
          await login(page);
          await page.route(/\/assets\/members-[^/]+\.js$/, route => route.fulfill({ status: 404, contentType: "text/plain", body: "fixture missing asset" }));
          await page.locator('nav a[href="/dashboard/members"]:visible').first().click();
          await expect(page.getByRole("alert").first()).toBeVisible();
          assert.equal(observed.calls.filter(call => call.path === "/admin/api/holo/members").length, 0);
          return { protected_reads: 0, business_effects: 0, result: "visible failure" };
        } finally { await context.close(); }
      });
    } finally { await browser.close(); }
  }
  report.status = "PASS_LOCAL_BROWSER"; console.log(`PRODUCTION BROWSER PASS: ${report.cases.length} cases, ${report.engines.length} engines`);
} catch (error) { report.status = "FAIL"; report.error = error.message; throw error; }
finally { await save(); if (worker) process.disconnect(); else { await origin.close(); await f.cleanup(); } }
