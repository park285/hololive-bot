import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { createServer } from "vite";
import { setTimeout as delay } from "node:timers/promises";

const run = promisify(execFile);
const root = fileURLToPath(new URL("../../", import.meta.url));

const entry = String.raw`
import React from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router";
import apiClient, { setCSRFToken, clearCSRFToken } from "/src/api/client.ts";
import { authApi } from "/src/api/core.ts";
import { useHeartbeat } from "/src/hooks/useHeartbeat.ts";
import { useAuthBootstrap } from "/src/hooks/useAuthBootstrap.ts";
import { useActivityDetection } from "/src/hooks/useActivityDetection.ts";
import { applySessionStatus, clearClientSession, refreshClientSession } from "/src/lib/sessionLifecycle.ts";
import { useAuthStore } from "/src/stores/authStore.ts";
import { AppLayout } from "/src/layouts/AppLayout.tsx";
import { getToastItems } from "/src/lib/toast-api.ts";

const tab = new URL(location.href).searchParams.get("tab");
const control = (action) => fetch("/__session_control/" + action, { method: "POST" }).then(r => r.json());
const wait = async (predicate, label) => {
  for (let i = 0; i < 500; i++) {
    if (await predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  throw new Error("timed out: " + label);
};
const expect = (value, label) => { if (!value) throw new Error(label); };

if (tab) {
  apiClient.defaults.baseURL = location.origin + "/__session_api";
  apiClient.defaults.headers.common["X-Test-Tab"] = tab;
  const originalHeartbeat = authApi.heartbeat;
  let refreshes = 0;
  let canceled = 0;
  const refreshed = () => {
    clearCSRFToken();
    void refreshClientSession().then(() => { refreshes++; }).catch(() => {});
  };
  const Tab = () => {
    useAuthBootstrap();
    const authenticated = useAuthStore(s => s.isAuthenticated);
    useActivityDetection({ enabled: authenticated, idleTimeoutMs: 600000, onSessionRefresh: refreshed, onRemoteLogout: () => clearClientSession() });
    useHeartbeat(false);
    return React.createElement(MemoryRouter, null, React.createElement(AppLayout));
  };
  createRoot(document.getElementById("root")).render(React.createElement(React.StrictMode, null, React.createElement(Tab)));
  window.contract = {
    authenticated: () => useAuthStore.getState().isAuthenticated,
    refreshes: () => refreshes,
    heartbeat: () => originalHeartbeat(false),
    getSession: () => authApi.getSession(),
    stale: () => setCSRFToken("stale-token"),
    mutate: () => apiClient.post("/settings", { minutes: 15 }),
    cancelHeartbeats: () => { authApi.heartbeat = async () => { canceled++; throw new DOMException("test cancellation", "AbortError"); }; },
    canceled: () => canceled,
    restoreHeartbeats: () => { authApi.heartbeat = originalHeartbeat; },
    loginAgain: async () => applySessionStatus(await authApi.getSession()),
    logout: () => document.querySelector('button[aria-label="로그아웃"]').click(),
    revocationWarning: () => getToastItems().some(item => String(item.message).includes("서버 세션 폐기를 확인하지 못했습니다")),
  };
} else {
  let a, b;
  try {
    await control("init");
    a = window.open("/__session_test?tab=A");
    b = window.open("/__session_test?tab=B");
    await wait(() => a?.contract && b?.contract, "two pages sharing cookies");
    await wait(async () => { const s = await control("state"); return s.accepted.A > 0 && s.accepted.B > 0; }, "initial heartbeats");

    await control("hold-b-get");
    const late = b.contract.getSession().then(() => "accepted", e => e.name);
    await wait(async () => (await control("state")).held, "held old GET");
    const beforeRefresh = b.contract.refreshes();
    const ownRefresh = a.contract.refreshes();
    await control("rotate");
    await a.contract.heartbeat();
    await wait(() => b.contract.refreshes() > beforeRefresh, "other page authoritative GET");
    expect(a.contract.refreshes() === ownRefresh, "rotation broadcast invalidated its own page");
    await control("release-b-get");
    expect(await late === "AbortError", "old GET overwrote a newer CSRF state");
    await b.contract.mutate();
    expect(a.contract.authenticated() && b.contract.authenticated(), "rotation logged out a page");

    for (let i = 0; i < 3; i++) {
      const before = await control("state");
      b.contract.stale();
      await wait(async () => (await control("state")).refused.B > before.refused.B, "stale-header rejection");
      await wait(async () => (await control("state")).accepted.B > before.accepted.B, "next scheduled heartbeat after GET recovery");
    }
    expect(a.contract.authenticated() && b.contract.authenticated(), "CSRF rejections became global logout");
    b.contract.cancelHeartbeats();
    await wait(() => b.contract.canceled() >= 3, "three canceled heartbeats");
    expect(b.contract.authenticated(), "cancellation counted as heartbeat failure");
    b.contract.restoreHeartbeats();

    await control("fail-logout");
    a.contract.logout();
    await wait(() => !a.contract.authenticated() && !b.contract.authenticated(), "local logout broadcast");
    expect(a.contract.revocationWarning(), "server revocation uncertainty was hidden");

    await control("init");
    await a.contract.loginAgain();
    await b.contract.loginAgain();
    await control("fail-heartbeat");
    await wait(() => !a.contract.authenticated(), "HTML/503 heartbeat failure threshold");
    document.body.dataset.testStatus = "passed";
    document.getElementById("root").textContent = "shared cookies; rotation; stale GET fence; 403 recovery; cancellation; logout uncertainty; heartbeat errors";
  } catch (error) {
    document.body.dataset.testStatus = "failed";
    document.getElementById("root").textContent = String(error?.stack || error);
  } finally {
    a?.close(); b?.close();
  }
}
`;

function fixture(state) {
  const session = () => ({ status: "ok", authenticated: true, username: "admin", absolute_expires_at: 2000000000, csrf_token: `csrf-${state.generation}`, session_policy: { heartbeat_interval_ms: 250, idle_timeout_ms: 600000, idle_warning_timeout_ms: 30000, idle_session_ttl_ms: 60000, absolute_warning_window_ms: 60000 } });
  const setCookies = (res, clear = false) => res.setHeader("Set-Cookie", [
    `admin_session=${clear ? "" : `session-${state.generation}`}; Path=/; HttpOnly; SameSite=Strict${clear ? "; Max-Age=0" : ""}`,
    `csrf_token=${clear ? "" : `csrf-${state.generation}`}; Path=/; HttpOnly; SameSite=Strict${clear ? "; Max-Age=0" : ""}`,
  ]);
  return {
    name: "session-tabs-contract",
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        const pathname = new URL(req.url, "http://localhost").pathname;
        if (pathname === "/__session_test") {
          res.setHeader("Content-Type", "text/html");
          res.end(await server.transformIndexHtml(pathname, '<!doctype html><body><div id="root"></div><script type="module" src="/__session_entry.jsx"></script></body>'));
          return;
        }
        if (!pathname.startsWith("/__session_api/") && !pathname.startsWith("/__session_control/")) return next();
        res.setHeader("Content-Type", "application/json");
        if (pathname.startsWith("/__session_control/")) {
          const action = pathname.split("/").at(-1);
          if (action === "init") { state.generation++; state.failLogout = false; state.failHeartbeat = false; setCookies(res); }
          if (action === "rotate") state.rotate = true;
          if (action === "hold-b-get") state.holdNext = true;
          if (action === "release-b-get") { state.release?.(); state.release = null; state.held = false; }
          if (action === "fail-logout") state.failLogout = true;
          if (action === "fail-heartbeat") state.failHeartbeat = true;
          res.end(JSON.stringify({ accepted: state.accepted, refused: state.refused, held: state.held }));
          return;
        }
        const tab = req.headers["x-test-tab"] ?? "A";
        const cookie = req.headers.cookie ?? "";
        if (pathname.endsWith("/auth/session")) {
          const body = JSON.stringify(session());
          if (tab === "B" && state.holdNext) { state.holdNext = false; state.held = true; state.release = () => res.end(body); return; }
          res.end(body);
          return;
        }
        if (pathname.endsWith("/auth/logout")) {
          state.logouts++;
          setCookies(res, true);
          res.statusCode = state.failLogout ? 503 : 200;
          res.end(JSON.stringify(state.failLogout ? { error: "Session store unavailable" } : { status: "ok" }));
          return;
        }
        if (pathname.endsWith("/auth/heartbeat") && state.failHeartbeat && tab === "A") {
          res.statusCode = 503; res.setHeader("Content-Type", "text/html"); res.end("<html>unavailable</html>"); return;
        }
        if (req.headers["x-csrf-token"] !== `csrf-${state.generation}` || !cookie.includes(`csrf_token=csrf-${state.generation}`) || !cookie.includes(`admin_session=session-${state.generation}`)) {
          state.refused[tab]++; res.statusCode = 403; res.end(JSON.stringify({ error: "Forbidden" })); return;
        }
        if (pathname.endsWith("/auth/heartbeat")) {
          state.accepted[tab]++;
          if (tab === "A" && state.rotate) {
            state.rotate = false; state.generation++; setCookies(res);
            res.end(JSON.stringify({ status: "ok", rotated: true, absolute_expires_at: 2000000000, csrf_token: `csrf-${state.generation}` }));
          } else res.end(JSON.stringify({ status: "ok", absolute_expires_at: 2000000000 }));
          return;
        }
        if (pathname.endsWith("/settings")) { state.mutations++; res.end(JSON.stringify({ status: "ok" })); return; }
        res.statusCode = 404; res.end(JSON.stringify({ error: "unexpected test request" }));
      });
    },
    resolveId(id) { if (id === "/__session_entry.jsx") return "\0virtual:session-tabs.jsx"; },
    load(id) { if (id === "\0virtual:session-tabs.jsx") return entry; },
  };
}

test("two real pages preserve session rotation, failure, and logout outcomes in one cookie jar", { timeout: 150000 }, async () => {
  let browser;
  for (const candidate of [process.env.CHROME_BIN, "/usr/bin/google-chrome", "/usr/bin/chromium"].filter(Boolean)) {
    try { await access(candidate); browser = candidate; break; } catch {}
  }
  assert(browser, "Chrome or Chromium is required");
  const profile = await mkdtemp(path.join(tmpdir(), "session-tabs-browser-"));
  const state = { generation: 0, accepted: { A: 0, B: 0 }, refused: { A: 0, B: 0 }, held: false, logouts: 0, mutations: 0 };
  const unit = `iris-session-browser-${process.pid}-${Date.now()}`;
  let server;
  let cdp;
  let started = false;
  try {
    server = await createServer({ root, configFile: path.join(root, "vite.config.ts"), logLevel: "error", plugins: [fixture(state)], server: { host: "127.0.0.1", port: 0 } });
    await server.listen();
    const address = server.httpServer.address();
    // 보류한 HTTP 응답과 실제 다중 탭의 wall clock을 사용하고 전체 Chrome process group을 정리한다.
    await run("systemd-run", ["--user", "--quiet", `--unit=${unit}`, "--property=RuntimeMaxSec=120", "--property=KillMode=control-group", "--property=TimeoutStopSec=5", browser,
      "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--disable-popup-blocking", "--disable-background-timer-throttling", "--disable-renderer-backgrounding", `--user-data-dir=${profile}`, "--remote-debugging-port=0", "about:blank"], { timeout: 10000 });
    started = true;
    let endpoint;
    for (let i = 0; i < 100; i++) {
      try { const [port, suffix] = (await readFile(path.join(profile, "DevToolsActivePort"), "utf8")).trim().split("\n"); endpoint = `ws://127.0.0.1:${port}${suffix}`; break; } catch (error) { if (error.code !== "ENOENT") throw error; }
      await delay(50);
    }
    assert(endpoint, "browser debugging endpoint did not start");
    cdp = await connectCDP(endpoint);
    const { targetId } = await cdp.call("Target.createTarget", { url: `http://127.0.0.1:${address.port}/__session_test` });
    const { sessionId } = await cdp.call("Target.attachToTarget", { targetId, flatten: true });
    await cdp.call("Runtime.enable", {}, sessionId);
    let result;
    const deadline = Date.now() + 90000;
    do {
      result = await cdp.call("Runtime.evaluate", { expression: "({status: document.body?.dataset.testStatus, html: document.body?.innerText})", returnByValue: true }, sessionId);
      if (result.result?.value?.status) break;
      await delay(100);
    } while (Date.now() < deadline);
    assert.equal(result.result?.value?.status, "passed", JSON.stringify({ result: result.result?.value, exceptions: cdp.exceptions, state }));
    assert.equal(state.mutations, 1);
    assert.equal(state.logouts, 2);
    assert(state.refused.B >= 3);
  } finally {
    state.release?.();
    cdp?.close();
    if (started) await run("systemctl", ["--user", "stop", unit], { timeout: 10000 });
    await server?.close();
    await rm(profile, { recursive: true, force: true });
  }
});

async function connectCDP(endpoint) {
  const socket = new WebSocket(endpoint);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  let sequence = 0;
  const pending = new Map();
  const exceptions = [];
  socket.addEventListener("message", event => {
    const message = JSON.parse(event.data);
    if (message.method === "Runtime.exceptionThrown") exceptions.push(message.params.exceptionDetails);
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    clearTimeout(request.timer);
    if (message.error) request.reject(new Error(JSON.stringify(message.error)));
    else request.resolve(message.result);
  });
  return {
    exceptions,
    call(method, params, sessionId) {
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        const timer = setTimeout(() => { pending.delete(id); reject(new Error(`CDP timeout: ${method}`)); }, 5000);
        pending.set(id, { resolve, reject, timer });
        socket.send(JSON.stringify({ id, method, params, sessionId }));
      });
    },
    close() {
      for (const request of pending.values()) { clearTimeout(request.timer); request.reject(new Error("CDP closed")); }
      pending.clear(); socket.close();
    },
  };
}
