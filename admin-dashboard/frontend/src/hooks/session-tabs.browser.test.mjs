import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { chromium, firefox, webkit, expect } from "@playwright/test";
import { createServer } from "vite";
import { wsServer } from "playwright-core/lib/utilsBundle";
import { setTimeout as delay } from "node:timers/promises";
import { CLIENT_GENERATION } from "../api/generated/generation.ts";

const root = fileURLToPath(new URL("../../", import.meta.url));
const run = promisify(execFile);
const entry = String.raw`
import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Routes, Route, useNavigate, useLocation } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { adminClient, bootstrapApplication, generation, httpClient, session, verifyMetadata, operations } from "/src/app/bootstrap.ts";
import { useBusinessMutation } from "/src/operations/useBusinessMutation.ts";
import { CLIENT_GENERATION } from "/src/api/generated/generation.ts";
import { createAdminClient } from "/src/api/client.ts";
import { SessionWarningPresentation } from "/src/session/AuthenticatedSession.tsx";
import { warningState } from "/src/session/warnings.ts";
import { QueryErrorBoundary } from "/src/components/QueryErrorBoundary.tsx";
import { useWebSocket } from "/src/queries/useWebSocket.ts";
import { AlarmGroups } from "/src/features/alarms/components/AlarmGroups.tsx";
import "/src/index.css";
import { useHeartbeat } from "/src/session/useHeartbeat.ts";
import { AppLayout } from "/src/layouts/AppLayout.tsx";
import { getToastItems } from "/src/lib/toast-api.ts";
import App from "/src/App.tsx";
import { RoomsPage } from "/src/features/rooms/pages/RoomsPage.tsx";
import { StreamsPage } from "/src/features/streams/pages/StreamsPage.tsx";
import { CalendarPage } from "/src/features/calendar/pages/CalendarPage.tsx";
import { StatsPage } from "/src/features/stats/pages/StatsPage.tsx";
import { SettingsPage } from "/src/features/settings/pages/SettingsPage.tsx";
import { ContainerList } from "/src/features/docker/components/ContainerList.tsx";
import { MembersPage } from "/src/features/members/pages/MembersPage.tsx";
import { AlarmsPage } from "/src/features/alarms/pages/AlarmsPage.tsx";
const params = new URL(location.href).searchParams;
httpClient.defaults.baseURL = location.origin;
httpClient.defaults.headers.common["X-Test-Tab"] = params.get("tab") ?? "boot";
const originalHeartbeat = session.heartbeat;
let canceled = 0;
let timers;
let setIdle;
let toggleWS;
let toggleAlarms;
let alarmEffects = [];
let frames = 0;
let navigateBusiness;
let currentRoute;
let businessCallbacks = 0;
let businessInvocations = 0;
let businessTransports = 0;
httpClient.interceptors.request.use(config => {
  if (config.method !== "get" && /\/(name|rooms)$/.test(config.url)) businessTransports++;
  return config;
}, undefined, { synchronous: true });
const testQueries = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 300000, refetchOnWindowFocus: false }, mutations: { retry: 2, retryDelay: 0 } } });
const ModuleProbe = React.lazy(async () => {
  if (sessionStorage.getItem("fixture-module-ready") !== "yes") throw new Error("fixture module unavailable");
  return { default: () => React.createElement("p", null, "화면 코드 복구 완료") };
});
const MemberSubmit = () => {
  const mutation = useBusinessMutation({ mutationFn: () => { businessInvocations++; return adminClient.holoUpdateMemberName("9007199254740993", { name: "한글 새 이름" }); }, onSuccess: () => { businessCallbacks++; } });
  return React.createElement("button", { disabled: mutation.isPending, onClick: () => mutation.mutate(undefined) }, "이름 저장");
};
const RoomSubmit = () => {
  const mutation = useBusinessMutation({ mutationFn: () => { businessInvocations++; return adminClient.holoAddRoom({ room: "9007199254740993" }); }, onError: () => {} });
  return React.createElement(React.Fragment, null, React.createElement("button", { disabled: mutation.isPending, onClick: () => mutation.mutate(undefined) }, "방 추가"),
    mutation.error && React.createElement("p", { role: "alert" }, mutation.error.message));
};
const Shell = () => {
  navigateBusiness = useNavigate();
  currentRoute = useLocation().pathname;
  return React.createElement(Routes, null, React.createElement(Route, { element: React.createElement(AppLayout) },
    React.createElement(Route, { path: "/dashboard/members", element: React.createElement(params.get("tab") === "LAYOUT" ? MembersPage : MemberSubmit) }),
    React.createElement(Route, { path: "/dashboard/rooms", element: React.createElement(params.get("tab") === "LAYOUT" ? RoomsPage : RoomSubmit) }),
    React.createElement(Route, { path: "/dashboard/member-editor", element: React.createElement(MembersPage) }),
    React.createElement(Route, { path: "/dashboard/alarm-editor", element: React.createElement(AlarmsPage) }),
    React.createElement(Route, { path: "/dashboard/docker-editor", element: React.createElement(ContainerList) }),
    React.createElement(Route, { path: "/dashboard/module-probe", element: React.createElement(React.Suspense, { fallback: "화면 준비 중" }, React.createElement(ModuleProbe)) }),
    ...[["rooms", RoomsPage], ["streams", StreamsPage], ["calendar", CalendarPage], ["stats", StatsPage]].map(([name, component]) => React.createElement(Route, { key: name, path: "/dashboard/" + name + "-view", element: React.createElement(component) })),
    ...[["stats", StatsPage], ["streams", StreamsPage], ["calendar", CalendarPage], ["alarms", AlarmsPage], ["settings", SettingsPage]].map(([name, component]) => React.createElement(Route, { key: name, path: "/dashboard/" + name, element: params.get("tab") === "LAYOUT" ? React.createElement(component) : null })),
    React.createElement(Route, { path: "*", element: null })));
};
const AlarmProbe = () => {
  const [expanded, setExpanded] = useState(new Set());
  const roomId = "9007199254740993";
  const alarms = Array.from({ length: 6 }, (_, index) => ({ roomId, roomName: "한글 방", channelId: "UC-" + index, memberName: "한글 멤버 " + index }));
  return React.createElement(AlarmGroups, {
    groups: [{ roomId, roomName: "한글 방", alarms }], expandedGroups: expanded, visibleGroupCount: 1, isDeleting: false,
    onToggleGroup: key => setExpanded(previous => previous.has(key) ? new Set() : new Set([key])),
    onEditName: (type, id) => { alarmEffects.push({ type, id }); },
    onDeleteAlarm: alarm => { alarmEffects.push({ type: "delete", roomId: alarm.roomId, channelId: alarm.channelId }); }, onLoadMore() {},
  });
};
const Stream = () => {
  useWebSocket(location.origin.replace("http", "ws") + "/admin/api/ws/system-stats", {
    protocol: "admin-stats." + CLIENT_GENERATION,
    beforeConnect: verifyMetadata,
    canConnect: () => generation.snapshot().phase === "ready" && session.state.snapshot().phase === "authenticated", reconnectInterval: 20,
    parseMessage: value => typeof value === "object" && value !== null && typeof value.sequence === "number" ? value : null,
    onMessage: () => { frames++; },
  });
  return null;
};
const Heartbeats = ({ idle }) => { useHeartbeat(idle); return null; };
const Harness = () => {
  const [enabled, setEnabled] = useState(false);
  const [idle, updateIdle] = useState(false);
  const [wsEnabled, setWS] = useState(false);
  const [alarmsEnabled, setAlarms] = useState(false);
  timers = setEnabled;
  setIdle = updateIdle;
  toggleWS = setWS;
  toggleAlarms = setAlarms;
  if (params.get("tab") === "WARNINGS") return React.createElement(QueryErrorBoundary, null, React.createElement(SessionWarningPresentation));
  if (alarmsEnabled) return React.createElement(AlarmProbe);
  return React.createElement(React.Fragment, null, enabled && React.createElement(Heartbeats, { idle }), wsEnabled && React.createElement(Stream),
    React.createElement(QueryClientProvider, { client: testQueries }, React.createElement(MemoryRouter, { initialEntries: [params.get("tab") === "LAYOUT" ? "/dashboard/layout-ready" : "/dashboard/stats"] }, React.createElement(Shell))));
};
createRoot(document.getElementById("root")).render(React.createElement(params.has("boot") ? App : Harness));
window.contract = {
  authenticated: () => session.state.snapshot().phase === "authenticated",
  resolved: () => session.state.snapshot().phase !== "pending",
  generation: () => generation.snapshot().phase,
  login: () => session.login("admin", "synthetic-password"),
  logout: () => session.logout(),
  heartbeat: () => session.heartbeat(),
  refresh: signal => session.refresh(signal),
  members: () => adminClient.holoGetMembers(),
  timers: value => timers(value),
  stale: () => session.state.acceptCSRF("stale-token", session.state.snapshot()),
  cancelHeartbeats: () => { session.heartbeat = async () => { canceled++; throw new DOMException("fixture cancellation", "AbortError"); }; },
  canceled: () => canceled,
  restoreHeartbeats: () => { session.heartbeat = originalHeartbeat; },
  busyNotice: () => getToastItems().some(item => String(item.message).includes("다른 업무 변경을 처리 중")),
  warning: () => getToastItems().some(item => String(item.message).includes("서버 세션 폐기를 확인하지 못했습니다")),
  stream: enabled => toggleWS(enabled),
  frames: () => frames,
  idle: value => setIdle(value),
  alarms: () => toggleAlarms(true),
  alarmEffects: () => alarmEffects,
  navigate: path => navigateBusiness(path),
  operation: () => operations.snapshot(),
  businessCallbacks: () => businessCallbacks,
  currentRoute: () => currentRoute,
  refetchEditors: () => testQueries.invalidateQueries(),
  invocationCounts: () => ({ functions: businessInvocations, transports: businessTransports }),
  prepareMutation: timeoutMs => {
    const probe = createAdminClient(location.origin, timeoutMs, {
      execute: (operation, work) => operations.execute(operation, work), context: session.state.snapshot,
      beforeRequest: () => generation.assertReady(), onUnauthorized: session.unauthorized, onGenerationError: generation.block,
    });
    window.preparationAbort = new AbortController();
    window.preparationResult = null;
    void probe.sdk.holoUpdateMemberName("9007199254740993", { name: "한글" }, { signal: window.preparationAbort.signal }).then(
      () => { window.preparationResult = "sent"; }, error => { window.preparationResult = typeof error.code === "string" ? error.code : error.name; });
  },
  abortPreparation: () => window.preparationAbort.abort(),
  changeSession: () => session.clearLocal(),
  openSessionWarning: kind => {
    if (kind === "idle") { warningState.getState().markSessionActivity(Date.now() - 540000); warningState.getState().openIdleWarning(); }
    else warningState.getState().openAbsoluteWarning();
  },
};
await bootstrapApplication();
window.contract.ready = true;
`;

const newState = () => ({ admissionClosed: false, memberCount: 1, readPages: false, readMode: "ok", readPagePosts: 0, rooms: ["9007199254740993"], aclEnabled: true, aclMode: "blacklist", docker: false, dockerAvailable: true, dockerMode: "confirmed", dockerState: "running", dockerPosts: 0, editors: false, readFailure: false, editorPosts: 0, member: { id: "9007199254740993", name: "한글 멤버", channelId: "UC1234567890123456789012", aliases: { ko: ["기존 별명"], ja: [] }, isGraduated: false }, roomName: "한글 방", alarmRemoved: false, generation: 0, alive: false, calls: [], hold: null, held: false, release: null, metaMode: "ok", failLogout: false, failHeartbeat: false, wsMode: "ok", wsRequests: [], clientFrames: 0, sockets: new Set(), businessMode: "ok", claims: new Set(), effects: { name: 0, rooms: 0 } });
const sessionBody = generation => ({ status: "ok", authenticated: true, username: "admin", absolute_expires_at: 2000000000, csrf_token: `csrf-${generation}`,
  session_policy: { heartbeat_interval_ms: 250, idle_timeout_ms: 600000, idle_warning_timeout_ms: 540000, idle_session_ttl_ms: 60000, absolute_warning_window_ms: 60000 } });
const cookies = generation => [`admin_session=session-${generation}; Path=/; HttpOnly; SameSite=Strict`, `csrf_token=csrf-${generation}; Path=/; HttpOnly; SameSite=Strict`];

function fixture(state) {
  return { name: "admin-session-contract-fixture", configureServer(vite) {
    vite.middlewares.use((req, res, next) => {
      const url = new URL(req.url, "http://fixture.test");
      if (url.pathname === "/__session_test") {
        void vite.transformIndexHtml(url.pathname, '<div id="root"></div><script type="module" src="/__session_entry.jsx"></script>').then(html => { res.setHeader("Content-Type", "text/html"); res.end(html); }).catch(next);
        return;
      }
      if (!url.pathname.startsWith("/admin/")) { next(); return; }
      const operation = url.pathname.split("/").at(-1);
      const call = { operation, query: url.search, method: req.method, tab: req.headers["x-test-tab"], port: req.socket.remotePort };
      state.calls.push(call);
      res.setHeader("Content-Type", "application/json");
      res.setHeader("Cache-Control", "no-store");
      res.setHeader("X-Admin-Server-Generation", CLIENT_GENERATION);
      const reply = (status, data, setCookies) => {
        const send = () => { res.statusCode = status; if (setCookies) res.setHeader("Set-Cookie", setCookies); res.end(JSON.stringify(data)); };
        if (state.hold === operation) { state.hold = null; state.held = true; state.release = () => { state.held = false; state.release = null; send(); }; }
        else send();
      };
      const failure = (status, code, notDispatched = false) => reply(status, { code, message: "Synthetic fixture failure", requestId: "fixture-request", ...(notDispatched ? { notDispatchedMutationId: req.headers["x-admin-mutation-id"] } : {}) });
      if (url.pathname === "/admin/meta.json") {
        if (state.metaMode === "once-unavailable") { state.metaMode = "ok"; failure(503, "SERVICE_UNAVAILABLE"); return; }
        if (state.metaMode === "missing") res.removeHeader("X-Admin-Server-Generation");
        if (state.metaMode === "old") res.setHeader("X-Admin-Server-Generation", "old");
        if (state.metaMode === "cached") res.removeHeader("Cache-Control");
        if (state.metaMode === "html") { res.setHeader("Content-Type", "text/html"); res.end("<h1>Unavailable</h1>"); return; }
        if (state.metaMode === "malformed") { res.end("{"); return; }
        reply(200, state.metaMode === "shape" ? {} : { clientGeneration: CLIENT_GENERATION }); return;
      }
      if (req.headers["x-admin-client-generation"] !== CLIENT_GENERATION) { failure(409, "CLIENT_GENERATION_MISMATCH"); return; }
      if (state.admissionClosed) { failure(503, "ADMISSION_CLOSED"); return; }
      if (operation === "login") {
        state.generation++; state.alive = true;
        reply(200, { status: "ok", message: "Login successful", csrf_token: `csrf-${state.generation}` }, cookies(state.generation)); return;
      }
      const cookie = req.headers.cookie ?? "";
      const authenticated = state.alive && cookie.split("; ").includes(`admin_session=session-${state.generation}`);
      if (!authenticated) { failure(401, "UNAUTHORIZED"); return; }
      if (operation === "session") { reply(200, sessionBody(state.generation), [cookies(state.generation)[1]]); return; }
      if (state.readPages && req.method === "GET" && ["rooms", "joined", "live", "upcoming", "calendar", "stats", "status", "settings"].includes(operation)) {
        if (state.readMode === "error") { failure(503, "SERVICE_UNAVAILABLE"); return; }
        const empty = state.readMode === "empty";
        const org = url.searchParams.get("org") ?? "hololive";
        const data = operation === "settings" ? { status: "ok", settings: { alarmAdvanceMinutes: 15 } } : operation === "rooms" ? { status: "ok", rooms: empty ? [] : state.rooms, aclEnabled: state.aclEnabled, aclMode: state.aclMode }
          : operation === "joined" ? { status: "ok", rooms: empty ? [] : [{ chatId: "9007199254740993", name: "한글 참여 방", type: "MultiChat", memberCount: 3 }] }
          : operation === "calendar" ? { status: "ok", year: Number(url.searchParams.get("year")), month: Number(url.searchParams.get("month")), entries: empty ? [] : [{ day: 9, kind: "birthday", member: { id: state.member.id, name: state.member.name, channelId: state.member.channelId } }] }
          : operation === "stats" ? { status: "ok", members: empty ? 0 : 10, alarms: empty ? 0 : 20, rooms: empty ? 0 : 3, version: "fixture", uptime: "1h" }
          : operation === "status" ? { version: "fixture", uptime: "1h", sampled_at: 1, services: empty ? [] : [{ name: "hololive-bot", available: true, response_time_ms: 10 }] }
          : { status: "ok", org, streams: empty ? [] : [{ id: "fixture-" + org, channel_id: state.member.channelId, channel_name: state.member.name, title: org + " 한글 방송", status: operation, thumbnail: null }] };
        reply(200, data); return;
      }
      if (state.docker && req.method === "GET" && ["health", "containers"].includes(operation)) {
        if (state.readFailure) { failure(503, "SERVICE_UNAVAILABLE"); return; }
        const containers = ["hololive-api", "holo-postgres"].map(name => ({ id: name + "-id", name, state: state.dockerState, status: "fixture", image: "fixture:local", managed: true, stopBlocked: name === "holo-postgres", created: 1, ports: [], health: "none" }));
        reply(200, operation === "health" ? { status: "ok", available: state.dockerAvailable } : { status: "ok", containers }); return;
      }
      if (state.editors && req.method === "GET" && ["members", "alarms"].includes(operation)) {
        if (state.readFailure) { failure(503, "SERVICE_UNAVAILABLE"); return; }
        reply(200, operation === "members" ? { status: "ok", members: Array.from({ length: state.memberCount }, (_, index) => ({ ...state.member, id: String(BigInt(state.member.id) + BigInt(index)), name: state.memberCount === 1 ? state.member.name : state.member.name + " " + String(index).padStart(3, "0") })) } : { status: "ok", alarms: state.alarmRemoved ? [] : [{ roomId: "9007199254740993", roomName: state.roomName, memberName: state.member.name, channelId: state.member.channelId }] }); return;
      }
      if (operation === "members" && !state.editors) { failure(401, "UNAUTHORIZED"); return; }
      if (req.headers["x-csrf-token"] !== `csrf-${state.generation}`) { failure(403, "FORBIDDEN"); return; }
      if (operation === "name" || operation === "rooms" || state.editors && ["members", "channel", "room", "aliases", "graduation", "alarms"].includes(operation) || state.docker && ["start", "stop", "restart"].includes(operation) || state.readPages && operation === "acl") {
        const mutationID = req.headers["x-admin-mutation-id"];
        if (typeof mutationID !== "string" || !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(mutationID)) { failure(400, "BAD_REQUEST"); return; }
        const claim = `${state.generation}:${mutationID}`;
        if (state.claims.has(claim)) { failure(409, "MUTATION_ALREADY_ATTEMPTED"); return; }
        state.claims.add(claim);
        let body = "";
        req.on("data", chunk => { body += chunk; });
        req.on("end", () => {
          if (state.docker) {
            state.dockerPosts++;
            if (state.dockerMode === "forbidden") { failure(403, "FORBIDDEN", true); return; }
            if (state.dockerMode === "server-error") { failure(502, "UPSTREAM_UNAVAILABLE"); return; }
            reply(200, state.dockerMode === "malformed" ? {} : { status: "ok" }); return;
          }
          const input = JSON.parse(body);
          if (state.readPages) {
            state.readPagePosts++;
            if (operation === "acl") {
              if (input.enabled !== undefined) state.aclEnabled = input.enabled;
              if (input.mode !== undefined) state.aclMode = input.mode;
              reply(200, { status: "ok", enabled: state.aclEnabled, mode: state.aclMode }); return;
            }
            if (req.method === "POST") state.rooms.push(input.room); else state.rooms = state.rooms.filter(room => room !== input.room);
            reply(200, { status: "ok" }); return;
          }
          if (state.editors) {
            state.editorPosts++;
            if (state.businessMode === "reject") { failure(403, "FORBIDDEN", true); return; }
            if (operation === "name") state.member.name = input.name;
            if (operation === "channel") state.member.channelId = input.channelId;
            if (operation === "room") state.roomName = input.roomName;
            if (operation === "aliases") state.member.aliases[input.type] = req.method === "DELETE" ? state.member.aliases[input.type].filter(alias => alias !== input.alias) : [...state.member.aliases[input.type], input.alias];
            if (operation === "graduation") state.member.isGraduated = input.isGraduated;
            if (operation === "alarms") { state.alarmRemoved = true; reply(200, { status: "ok", removed: true }); return; }
            reply(200, { status: "ok" }); return;
          }
          state.effects[operation]++;
          if (state.businessMode === "lost" || state.businessMode === "lost-closed") { state.admissionClosed = state.businessMode === "lost-closed"; res.destroy(); return; }
          reply(200, state.businessMode === "malformed" ? {} : { status: "ok" });
        });
        return;
      }
      if (operation === "heartbeat") {
        let body = "";
        req.on("data", chunk => { body += chunk; });
        req.on("end", () => {
          call.idle = JSON.parse(body).idle;
          if (call.idle) { reply(200, { status: "idle", idle_rejected: true }); return; }
          if (state.failHeartbeat) { failure(503, "SERVICE_UNAVAILABLE"); return; }
          state.generation++;
          reply(200, { status: "ok", rotated: true, csrf_token: `csrf-${state.generation}`, absolute_expires_at: 2000000000 }, cookies(state.generation));
        });
        return;
      }
      if (operation === "logout") {
        const setCookies = ["admin_session=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0", "csrf_token=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0"];
        if (state.failLogout) reply(503, { code: "SERVICE_UNAVAILABLE", message: "Unavailable", requestId: "fixture-request" }, setCookies);
        else { state.alive = false; reply(200, { status: "ok" }, setCookies); }
        return;
      }
      failure(404, "NOT_FOUND");
    });
  }, resolveId(id) { if (id === "/__session_entry.jsx") return id; }, load(id) { if (id === "/__session_entry.jsx") return entry; } };
}

async function ready(page, url) {
  const errors = [];
  const requests = [];
  const failed = request => {
    if (requests.length < 16) requests.push({ path: new URL(request.url()).pathname, failure: request.failure()?.errorText });
  };
  const responded = response => {
    if (response.status() >= 400 && requests.length < 16) requests.push({ path: new URL(response.url()).pathname, status: response.status() });
  };
  page.on("pageerror", error => { errors.push(error.message); });
  page.on("requestfailed", failed);
  page.on("response", responded);
  try {
    await page.goto(url);
    await page.waitForFunction(() => window.contract?.ready, undefined, { timeout: 10000 });
  }
  catch (error) {
    const snapshot = await page.evaluate(() => ({
      readyState: document.readyState, visibility: document.visibilityState, href: location.href,
      contractPresent: !!window.contract, contractReady: window.contract?.ready,
      generation: window.contract?.generation?.(), sessionResolved: window.contract?.resolved?.(),
      resources: performance.getEntriesByType("resource").slice(-15).map(entry => ({ path: new URL(entry.name).pathname, duration: entry.duration, responseStatus: entry.responseStatus }))
    })).catch(error => ({ unavailable: error.message }));
    throw new Error(`browser startup failed: ${errors.join("; ")}; snapshot=${JSON.stringify(snapshot)}; requests=${JSON.stringify(requests)}`, { cause: error });
  } finally {
    page.off("requestfailed", failed);
    page.off("response", responded);
  }
}
async function login(page) {
  // 조회는 잠금을 기다릴 수 있으므로 그 조회가 끝난 뒤 명시적으로 로그인합니다.
  await page.evaluate(() => window.contract.refresh().catch(() => undefined));
  await page.evaluate(() => window.contract.login());
  await expect.poll(() => page.evaluate(() => window.contract.authenticated())).toBe(true);
}

async function exerciseValidationPreparation(browser, base, state) {
  for (const failure of ["timeout", "abort", "session", "module-error"]) {
    const context = await browser.newContext(), page = await context.newPage();
    let held;
    try {
      await ready(page, base + "/__session_test?tab=VALIDATION"); await login(page);
      await page.route("**/src/api/generated/validators/members.mjs*", route => { held = route; });
      const before = { ...state.effects };
      await page.evaluate(timeout => window.contract.prepareMutation(timeout), failure === "timeout" ? 250 : 5000);
      await expect.poll(() => held !== undefined).toBe(true);
      if (failure === "abort") await page.evaluate(() => window.contract.abortPreparation());
      if (failure === "session") await page.evaluate(() => window.contract.changeSession());
      if (failure === "module-error") await held.abort("failed");
      else if (failure === "session") await held.continue();
      await expect.poll(() => page.evaluate(() => window.preparationResult)).toBe(failure === "abort" || failure === "session" ? "AbortError" : "VALIDATION_UNAVAILABLE");
      if (failure === "timeout" || failure === "abort") {
        const received = page.waitForResponse(response => response.url().includes("/generated/validators/members.mjs"));
        await held.continue(); await received;
        await page.waitForTimeout(100);
      }
      assert.deepEqual(state.effects, before, "a late or failed validation module must not dispatch a mutation");
      assert.equal(await page.evaluate(() => window.contract.operation().busy), false);
    } finally { await context.close(); }
  }
}

async function exerciseWarningLoading(browser, base) {
  for (const outcome of ["loaded", "session-changed", "module-error"]) {
    const context = await browser.newContext(), page = await context.newPage();
    let held, requests = 0;
    try {
      await page.route("**/src/session/SessionWarningDialogs.tsx*", route => { requests++; held = route; });
      await ready(page, base + "/__session_test?tab=WARNINGS"); await login(page);
      assert.equal(requests, 0, "warning UI is not needed during normal startup");
      await page.evaluate(() => window.contract.openSessionWarning("idle"));
      await expect.poll(() => Boolean(held)).toBe(true);
      await expect(page.getByRole("alert")).toContainText("세션 만료가 임박했습니다");
      if (outcome === "module-error") {
        await held.abort("failed");
        await expect(page.getByRole("heading", { name: "문제가 발생했습니다" })).toBeVisible();
      } else {
        if (outcome === "session-changed") await page.evaluate(() => window.contract.changeSession());
        await held.continue();
        if (outcome === "session-changed") await expect(page.getByRole("dialog")).toHaveCount(0);
        else {
          const idle = page.getByRole("dialog", { name: "곧 자동 로그아웃됩니다" });
          await expect(idle).toBeVisible();
          assert.equal(await idle.evaluate(element => element.contains(document.activeElement)), true);
          await idle.getByRole("button", { name: "세션 연장", exact: true }).click();
          await expect(idle).toHaveCount(0);
          await page.evaluate(() => window.contract.openSessionWarning("absolute"));
          const absolute = page.getByRole("dialog", { name: "세션이 곧 만료됩니다" });
          await expect(absolute).toBeVisible();
          await absolute.getByRole("button", { name: "확인했습니다" }).click();
          await expect(absolute).toHaveCount(0);
          assert.equal(requests, 1, "later warnings reuse the prepared module");
        }
      }
    } finally { await context.close(); }
  }
}

async function exerciseCookies(browser, base, state) {
  const context = await browser.newContext();
  const a = await context.newPage(), b = await context.newPage();
  try {
    await ready(a, `${base}/__session_test?tab=A`);
    await ready(b, `${base}/__session_test?tab=B`);
    await login(a);
    await expect.poll(() => b.evaluate(() => window.contract.authenticated())).toBe(true);
    for (const operation of ["session", "heartbeat", "logout"]) {
      state.hold = operation;
      await b.evaluate(operation => {
        const method = operation === "session" ? "refresh" : operation;
        window.refreshAbort = new AbortController();
        window.pending = window.contract[method](operation === "session" ? window.refreshAbort.signal : undefined).then(value => ({ value }), error => ({ error: error.name }));
      }, operation);
      await expect.poll(() => state.held).toBe(true);
      if (operation === "session") await b.evaluate(() => window.refreshAbort.abort());
      const before = state.calls.filter(call => call.operation === "login").length;
      const busy = await a.evaluate(() => window.contract.login().then(() => "sent", error => error.code));
      assert.equal(busy, "SESSION_BUSY");
      assert.equal(state.calls.filter(call => call.operation === "login").length, before, "login must not wait or dispatch behind a cookie writer");
      state.release();
      const result = await b.evaluate(() => window.pending);
      if (operation === "session") assert.equal(result.error, "AbortError", "canceled read must hold the cookie lock until its response settles");
      else assert(!result.error, JSON.stringify(result));
      await login(a);
      await expect.poll(async () => {
        const jar = await context.cookies();
        return jar.find(cookie => cookie.name === "admin_session")?.value;
      }).toBe(`session-${state.generation}`);
      await expect.poll(() => b.evaluate(() => window.contract.authenticated())).toBe(true);
    }
    // 쿠키를 쓰지 않는 이전 업무 조회 401은 새로운 로그인과 겹쳐도 현재 세션을 지우지 않습니다.
    state.hold = "members";
    await b.evaluate(() => { window.pending = window.contract.members().catch(error => error.response?.status); });
    await expect.poll(() => state.held).toBe(true);
    await login(a);
    state.release();
    assert.equal(await b.evaluate(() => window.pending), 401);
    await expect.poll(() => b.evaluate(() => window.contract.authenticated())).toBe(true);
    const current = state.generation;
    assert.equal((await context.cookies()).find(cookie => cookie.name === "admin_session")?.value, `session-${current}`);

    // 기존 403 조회 복구·취소 제외·3회 실패 종료의 의미도 실제 hook에서 보존합니다.
    await a.evaluate(() => window.contract.timers(true));
    await expect.poll(() => state.calls.filter(call => call.operation === "heartbeat").length).toBeGreaterThan(1);
    await a.evaluate(() => window.contract.stale());
    const reads = state.calls.filter(call => call.operation === "session").length;
    await expect.poll(() => state.calls.filter(call => call.operation === "session").length).toBeGreaterThan(reads);
    assert(await a.evaluate(() => window.contract.authenticated()));
    await a.evaluate(() => window.contract.cancelHeartbeats());
    await expect.poll(() => a.evaluate(() => window.contract.canceled())).toBeGreaterThanOrEqual(3);
    assert(await a.evaluate(() => window.contract.authenticated()));
    await a.evaluate(() => { window.contract.restoreHeartbeats(); window.contract.timers(false); });
    await a.evaluate(() => window.contract.refresh());
    state.failLogout = true;
    await a.getByRole("button", { name: "로그아웃", exact: true }).click();
    await expect.poll(() => a.evaluate(() => window.contract.authenticated())).toBe(false);
    await expect.poll(() => b.evaluate(() => window.contract.authenticated())).toBe(false);
    assert(await a.evaluate(() => window.contract.warning()), "logout uncertainty must be visible");
    state.failLogout = false;
    await login(a);
    state.failHeartbeat = true;
    const failures = state.calls.filter(call => call.operation === "heartbeat").length;
    await a.evaluate(() => window.contract.timers(true));
    await expect.poll(() => a.evaluate(() => window.contract.authenticated())).toBe(false);
    assert.equal(state.calls.filter(call => call.operation === "heartbeat").length - failures, 3);
    state.failHeartbeat = false;
    await a.evaluate(() => window.contract.timers(false));
    await login(a);
    const idleRequests = state.calls.length;
    await a.evaluate(() => { window.contract.idle(true); window.contract.timers(true); });
    await expect.poll(() => a.evaluate(() => window.contract.authenticated())).toBe(false);
    const idleHeartbeats = state.calls.slice(idleRequests).filter(call => call.operation === "heartbeat");
    assert.equal(idleHeartbeats.length, 1, "idle transition must send once immediately and revoke before the next scheduled heartbeat");
    assert.equal(idleHeartbeats[0].idle, true);
  } finally { state.release?.(); await context.close(); }
}

async function exerciseMetadata(browser, base, state) {
  for (const mode of ["missing", "old", "html", "malformed", "shape", "cached"]) {
    Object.assign(state, newState(), { metaMode: mode });
    const context = await browser.newContext();
    const page = await context.newPage();
    const diagnostics = [];
    page.on("console", message => { if (message.text().includes("vite")) diagnostics.push(message.text()); });
    page.on("framenavigated", frame => { if (frame === page.mainFrame()) diagnostics.push("navigate: " + frame.url()); });
    try {
      await ready(page, `${base}/__session_test?boot=1`);
      assert.notEqual(await page.evaluate(() => window.contract.generation()), "ready", mode);
      assert.deepEqual(state.calls.map(call => call.operation), ["meta.json"], `${mode} must stop before authentication or business I/O: ${JSON.stringify(diagnostics)}`);
      await expect(page.getByRole("button", { name: mode === "missing" || mode === "old" ? "새로고침" : "다시 확인", exact: true })).toBeVisible();
      if (mode === "cached") {
        state.metaMode = "ok";
        state.hold = "session";
        await page.getByRole("button", { name: "다시 확인", exact: true }).click();
        await expect.poll(() => state.held).toBe(true);
        assert.equal(await page.evaluate(() => window.contract.resolved()), false, "manual bootstrap retry must keep protected UI pending until the session reply");
        state.release();
        await expect.poll(() => page.evaluate(() => window.contract.resolved())).toBe(true);
        assert.equal(await page.evaluate(() => window.contract.authenticated()), false);
      }
    } finally { await context.close(); }
  }
}

async function exerciseStreams(browser, base, state) {
  Object.assign(state, newState());
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await ready(page, `${base}/__session_test?tab=WS`);
    await login(page);
    state.wsMode = "missing";
    const initialMetadata = state.calls.filter(call => call.operation === "meta.json").length;
    await page.evaluate(() => window.contract.stream(true));
    await expect.poll(() => state.wsRequests.length).toBe(6);
    await delay(100);
    assert.equal(state.wsRequests.length, 6, "initial handshake plus five retries is the existing limit");
    assert.equal(state.calls.filter(call => call.operation === "meta.json").length - initialMetadata, 6);
    assert.equal(await page.evaluate(() => window.contract.frames()), 0, "unnegotiated frames must never reach the consumer");
    assert(state.wsRequests.every(protocol => protocol === `admin-stats.${CLIENT_GENERATION}`));
    await page.evaluate(() => window.contract.stream(false));
    state.wsMode = "ok";
    state.metaMode = "once-unavailable";
    await page.evaluate(() => window.contract.stream(true));
    await expect.poll(() => page.evaluate(() => window.contract.frames())).toBe(1);
    assert.equal(await page.evaluate(() => window.contract.generation()), "ready", "ordinary metadata failure is not a confirmed generation mismatch");
    const connected = state.wsRequests.length;
    state.metaMode = "old";
    for (const socket of state.sockets) socket.close();
    await expect.poll(() => page.evaluate(() => window.contract.generation())).toBe("incompatible");
    await delay(150);
    assert.equal(state.wsRequests.length, connected, "metadata mismatch must prevent the reconnect handshake");
    const before = state.calls.length;
    assert.equal(await page.evaluate(() => window.contract.members().then(() => "sent", error => error.code)), "CLIENT_NOT_READY");
    assert.equal(state.calls.length, before);
    assert.equal(await page.evaluate(() => window.contract.frames()), 1);
    assert.equal(state.clientFrames, 0, "the stats subscription sends no application data frames");
  } finally { await context.close(); }
}

async function exerciseAlarmControls(browser, base, state) {
  Object.assign(state, newState());
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await ready(page, `${base}/__session_test?tab=ALARMS`);
    await page.evaluate(() => window.contract.alarms());
    const disclosure = page.getByRole("button", { name: "한글 방 알람 그룹 펼치기", exact: true });
    const edit = page.getByRole("button", { name: "한글 방 방 이름 수정", exact: true });
    await expect(disclosure).toHaveAttribute("aria-expanded", "false");
    await edit.click();
    await expect(disclosure).toHaveAttribute("aria-expanded", "false");
    assert.deepEqual(await page.evaluate(() => window.contract.alarmEffects()), [{ type: "room", id: "9007199254740993" }]);
    await disclosure.focus();
    await page.keyboard.press("Space");
    await expect(page.getByRole("button", { name: "한글 방 알람 그룹 접기", exact: true })).toHaveAttribute("aria-expanded", "true");
    await page.getByRole("button", { name: "한글 멤버 5 알람 삭제", exact: true }).click();
    assert.deepEqual((await page.evaluate(() => window.contract.alarmEffects())).at(-1), { type: "delete", roomId: "9007199254740993", channelId: "UC-5" });
    assert.equal(await page.getByRole("button", { name: /유저 이름 수정/ }).count(), 0);
  } finally { await context.close(); }
}

async function exerciseBusinessMutations(browser, base, state) {
  Object.assign(state, newState());
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await ready(page, `${base}/__session_test?tab=BUSINESS`);
    await login(page);
    await page.evaluate(() => window.contract.navigate("/dashboard/members"));
    state.hold = "name";
    await page.getByRole("button", { name: "이름 저장", exact: true }).click();
    await expect.poll(() => state.held).toBe(true);
    await page.evaluate(() => window.contract.navigate("/dashboard/rooms"));
    await expect(page.getByRole("complementary", { name: "업무 변경 결과" })).toContainText("멤버 이름 변경 처리 중");
    await page.getByRole("button", { name: "방 추가", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("다른 업무 변경을 처리 중");
    assert.equal(await page.evaluate(() => window.contract.busyNotice()), true, "feature error callbacks cannot suppress the global BUSY notice");
    assert.equal(state.effects.rooms, 0);
    state.release();
    await expect(page.getByRole("complementary", { name: "업무 변경 결과" })).toContainText("요청한 변경을 완료했습니다");
    await delay(100);
    assert.equal(state.effects.rooms, 0, "BUSY may not queue across a route change");

    await context.setOffline(true);
    await page.getByRole("button", { name: "방 추가", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("오프라인에서는 변경 요청을 보내지 않습니다");
    assert.equal(state.effects.rooms, 0);
    await context.setOffline(false);
    await delay(150);
    assert.equal(state.effects.rooms, 0, "reconnection cannot replay an offline mutation");
    await page.getByRole("button", { name: "방 추가", exact: true }).click();
    await expect.poll(() => state.effects.rooms).toBe(1);
    await expect.poll(() => page.evaluate(() => window.contract.operation().busy)).toBe(false);

    for (const mode of ["malformed", "lost", "lost-closed"]) {
      await page.evaluate(() => window.contract.refresh());
      state.businessMode = mode;
      const before = state.effects.rooms;
      const beforeCalls = state.calls.length;
      const invocations = await page.evaluate(() => window.contract.invocationCounts());
      await page.getByRole("button", { name: "방 추가", exact: true }).click();
      await expect.poll(() => page.evaluate(() => window.contract.operation().busy)).toBe(false);
      assert.equal(await page.evaluate(() => window.contract.operation().result?.outcome.kind), "unknown", JSON.stringify({ mode, effects: state.effects.rooms - before, calls: state.calls.slice(beforeCalls) }));
      await delay(150);
      const afterInvocations = await page.evaluate(() => window.contract.invocationCounts());
      assert.equal(afterInvocations.functions - invocations.functions, 1);
      assert.equal(afterInvocations.transports - invocations.transports, 1);
      assert.equal(state.effects.rooms - before, 1, JSON.stringify({ rule: "unknown result cannot replay", mode, before: invocations, after: await page.evaluate(() => window.contract.invocationCounts()), calls: state.calls.slice(beforeCalls) }));
    }

    state.admissionClosed = false;
    state.businessMode = "ok";
    state.hold = "name";
    await page.evaluate(() => window.contract.navigate("/dashboard/members"));
    const callbacks = await page.evaluate(() => window.contract.businessCallbacks());
    await page.getByRole("button", { name: "이름 저장", exact: true }).click();
    await expect.poll(() => state.held).toBe(true);
    await page.evaluate(() => window.contract.login());
    assert.deepEqual(await page.evaluate(() => window.contract.operation()), { busy: true, activeLabel: null, result: null });
    state.release();
    await expect.poll(() => page.evaluate(() => window.contract.operation().busy)).toBe(false);
    assert.equal(await page.evaluate(() => window.contract.businessCallbacks()), callbacks, "old mutation completion cannot call a newer session's UI handler");
    assert.equal(await page.evaluate(() => window.contract.operation().result), null);
  } finally { state.release?.(); await context.close(); }
}


async function exerciseEditors(browser, base, state) {
  Object.assign(state, newState(), { editors: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  try {
    await ready(page, base + "/__session_test?tab=EDITORS"); await login(page);
    await page.evaluate(() => window.contract.navigate("/dashboard/member-editor"));
    await page.getByRole("button", { name: "한글 멤버 이름 수정", exact: true }).click();
    let dialog = page.getByRole("dialog", { name: "멤버 이름 수정" });
    const nameInput = dialog.getByLabel("새로운 이름");
    await nameInput.fill("작성 중인 이름");
    state.member.name = "외부 변경 이름";
    await page.evaluate(() => window.contract.refetchEditors());
    await expect(nameInput).toHaveValue("작성 중인 이름");
    await expect(dialog).toContainText("편집 중 저장된 값이 변경되었습니다");
    await expect(dialog.getByRole("button", { name: "저장", exact: true })).toBeDisabled();
    await dialog.getByRole("button", { name: "이 초안으로 계속", exact: true }).click();
    state.businessMode = "reject";
    await dialog.getByRole("button", { name: "저장", exact: true }).click();
    await expect(dialog).toContainText("서버가 요청을 거절했습니다");
    await expect(nameInput).toHaveValue("작성 중인 이름");
    state.readFailure = true; await page.evaluate(() => window.contract.refetchEditors());
    await expect(dialog.getByRole("button", { name: "저장", exact: true })).toBeDisabled();
    await expect(dialog).toContainText("조회 결과를 확인하지 못했습니다");
    state.readFailure = false; state.businessMode = "ok";
    await dialog.getByRole("button", { name: "다시 조회", exact: true }).click();
    state.hold = "name";
    await dialog.getByRole("button", { name: "저장", exact: true }).click();
    await expect.poll(() => state.held).toBe(true);
    await expect(dialog).toBeVisible();
    await expect(nameInput).toBeDisabled();
    await expect(dialog.getByRole("button", { name: "저장", exact: true })).toBeDisabled();
    await dialog.getByRole("button", { name: "취소", exact: true }).click();
    await page.getByRole("button", { name: "외부 변경 이름 이름 수정", exact: true }).click();
    state.release();
    await expect(page.getByRole("button", { name: "작성 중인 이름 이름 수정", exact: true })).toBeVisible();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "취소", exact: true }).click();

    await page.getByRole("button", { name: "작성 중인 이름 채널 ID 수정", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "채널 ID 수정" });
    await dialog.getByLabel("YouTube 채널 ID").fill("UC987654321098765432109876");
    state.businessMode = "reject";
    state.hold = "channel";
    await dialog.getByRole("button", { name: "저장", exact: true }).click();
    await expect.poll(() => state.held).toBe(true);
    await expect(dialog.getByLabel("YouTube 채널 ID")).toBeDisabled();
    state.release();
    await expect(dialog).toContainText("서버가 요청을 거절했습니다");
    await expect(dialog.getByLabel("YouTube 채널 ID")).toBeEnabled();
    await expect(dialog.getByLabel("YouTube 채널 ID")).toHaveValue("UC987654321098765432109876");
    state.member.channelId = "UC555555555555555555555555";
    await page.evaluate(() => window.contract.refetchEditors());
    await expect(dialog).toContainText("편집 중 저장된 값이 변경되었습니다");
    await dialog.getByRole("button", { name: "최신 값 사용", exact: true }).click();
    await expect(dialog.getByLabel("YouTube 채널 ID")).toHaveValue(state.member.channelId);
    await dialog.getByRole("button", { name: "취소", exact: true }).click();

    await page.getByRole("button", { name: "새로운 멤버 추가", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "새 멤버 추가" });
    await dialog.getByLabel("멤버 이름 (기본)").fill("신규 멤버 초안");
    await dialog.getByLabel("YouTube 채널 ID").fill("UC123456789012345678901234");
    await dialog.getByRole("button", { name: "새 멤버 정보 저장 및 추가" }).click();
    await expect(dialog).toContainText("서버가 요청을 거절했습니다");
    await expect(dialog.getByLabel("멤버 이름 (기본)")).toHaveValue("신규 멤버 초안");
    await dialog.getByRole("button", { name: "취소", exact: true }).click();

    await page.evaluate(() => window.contract.navigate("/dashboard/alarm-editor"));
    await page.getByRole("button", { name: "한글 방 방 이름 수정", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "방 이름 수정" });
    await dialog.getByLabel("새로운 이름").fill("방 이름 초안");
    state.roomName = "외부 변경 방"; await page.evaluate(() => window.contract.refetchEditors());
    await expect(dialog).toContainText("편집 중 저장된 값이 변경되었습니다");
    await dialog.getByRole("button", { name: "이 초안으로 계속", exact: true }).click();
    await dialog.getByRole("button", { name: "저장", exact: true }).click();
    await expect(dialog).toContainText("서버가 요청을 거절했습니다");
    await expect(dialog.getByLabel("새로운 이름")).toHaveValue("방 이름 초안");
    assert.equal(state.editorPosts, 5, "rejections and external edits cannot replay a mutation");
    await dialog.getByRole("button", { name: "취소", exact: true }).click();
    state.businessMode = "ok";
    await page.getByRole("button", { name: "외부 변경 방 알람 그룹 펼치기", exact: true }).click();
    await page.getByRole("button", { name: "작성 중인 이름 알람 삭제", exact: true }).click();
    const deletion = page.getByRole("dialog", { name: "알람 삭제" });
    await expect(deletion.getByRole("button", { name: "취소", exact: true })).toBeFocused();
    await deletion.getByRole("button", { name: "삭제", exact: true }).click();
    await expect(page.getByText("등록된 알람이 없습니다.", { exact: true })).toBeVisible();
    await page.evaluate(() => window.contract.navigate("/dashboard/member-editor"));
    await page.getByLabel("작성 중인 이름 한국어 별명 입력").fill("새 별명");
    await page.getByRole("button", { name: "한국어 별명 추가", exact: true }).click();
    await expect(page.getByLabel("작성 중인 이름 한국어 별명 입력")).toHaveValue("");
    await page.getByRole("button", { name: "작성 중인 이름 한국어 별명 새 별명 제거", exact: true }).click();
    await page.getByRole("dialog", { name: "별명 삭제" }).getByRole("button", { name: "삭제", exact: true }).click();
    await expect.poll(() => state.member.aliases.ko.includes("새 별명")).toBe(false);
    await page.getByRole("button", { name: "작성 중인 이름 졸업 처리", exact: true }).click();
    await page.getByRole("dialog", { name: "졸업 처리" }).getByRole("button", { name: "확인", exact: true }).click();
    await expect.poll(() => state.member.isGraduated).toBe(true);
    await page.getByRole("checkbox", { name: "졸업 멤버 숨기기" }).uncheck();
    await page.getByRole("button", { name: "작성 중인 이름 졸업 해제 및 복귀", exact: true }).click();
    await page.getByRole("dialog", { name: "졸업 해제 (복귀)" }).getByRole("button", { name: "확인", exact: true }).click();
    await expect.poll(() => state.member.isGraduated).toBe(false);
    assert.equal(state.editorPosts, 10, "alarm removal, aliases and graduation/reinstatement execute once each");
    assert.deepEqual(errors, [], "editor failures cannot produce unhandled promises");
  } finally { state.release?.(); await context.close(); }
}


async function exerciseDocker(browser, base, state) {
  Object.assign(state, newState(), { docker: true });
  const context = await browser.newContext(); const page = await context.newPage();
  try {
    await ready(page, base + "/__session_test?tab=DOCKER"); await login(page);
    await page.evaluate(() => window.contract.navigate("/dashboard/docker-editor"));
    await expect(page.getByRole("button", { name: "인프라 컨테이너는 중지할 수 없습니다", exact: true })).toBeDisabled();
    for (const [action, label] of [["restart", "재시작"], ["stop", "중지"], ["start", "시작"]]) {
      state.dockerState = action === "start" ? "exited" : "running";
      await page.evaluate(() => window.contract.refetchEditors());
      const trigger = page.getByRole("button", { name: "hololive-api 컨테이너 " + label, exact: true });
      for (const mode of ["confirmed", "malformed", "server-error", "forbidden"]) {
        state.dockerMode = mode;
        const before = state.dockerPosts;
        await trigger.click();
        const dialog = page.getByRole("dialog", { name: "컨테이너 " + label, exact: true });
        await expect(dialog.getByRole("button", { name: "취소", exact: true })).toBeFocused();
        await page.keyboard.press("Escape");
        assert.equal(state.dockerPosts, before, "cancelling the default focus cannot send a mutation");
        await trigger.click();
        await dialog.getByRole("button", { name: label + " 실행", exact: true }).click();
        await expect.poll(() => state.dockerPosts).toBe(before + 1);
        await expect.poll(() => page.evaluate(() => window.contract.operation().busy)).toBe(false);
        const expected = mode === "confirmed" ? "succeeded" : mode === "forbidden" ? "rejected" : "unknown";
        await expect.poll(() => page.evaluate(() => window.contract.operation().result?.outcome.kind)).toBe(expected);
        await expect(trigger).toBeEnabled();
        assert.equal(state.dockerPosts, before + 1, "no inherited retry after a Docker result");
      }
    }
    state.readFailure = true;
    await page.evaluate(() => window.contract.refetchEditors());
    await expect(page.getByRole("button", { name: "hololive-api 컨테이너 시작", exact: true })).toBeDisabled();
    await expect(page.getByText("Docker 상태 미확인", { exact: true })).toBeVisible();
    assert.equal(state.dockerPosts, 12);
  } finally { await context.close(); }
}


async function exerciseReadPages(browser, base, state) {
  Object.assign(state, newState(), { readPages: true });
  const context = await browser.newContext(); const page = await context.newPage();
  try {
    await ready(page, base + "/__session_test?tab=READS"); await login(page);
    for (const [feature, label] of [["rooms", "채팅방 접근 설정"], ["streams", "진행 중인 방송"], ["calendar", "기념일 달력"], ["stats", "Hololive 통계"]]) {
      state.readMode = "error";
      await page.evaluate(feature => window.contract.navigate("/dashboard/" + feature + "-view"), feature);
      // 이전 화면의 같은 오류 문구로 통과하면 새 query가 등록되기 전에 refetch할 수 있습니다.
      await expect(page.getByRole("alert").filter({ has: page.getByText(label, { exact: true }) })).toContainText("조회 결과를 확인하지 못했습니다.");
      if (feature === "rooms") assert.equal(await page.getByRole("switch").count(), 0, "failed ACL reads cannot invent a switch state");
      if (feature === "stats") assert.equal(await page.getByText("등록된 멤버", { exact: true }).count(), 0, "failed stats cannot invent zero counters");
      state.readMode = "ok"; await page.evaluate(() => window.contract.refetchEditors());
      const known = feature === "rooms" ? "한글 참여 방" : feature === "streams" ? "hololive 한글 방송" : feature === "calendar" ? "한글 멤버" : "등록된 멤버";
      await expect(page.getByText(known, { exact: true }).first()).toBeVisible();
      state.readMode = "error"; await page.evaluate(() => window.contract.refetchEditors());
      await expect(page.getByText(known, { exact: true }).first()).toBeVisible();
      await expect(page.getByText("마지막으로 확인한 값을 표시합니다.", { exact: true }).first()).toBeVisible();
      await context.setOffline(true);
      await expect(page.getByText("오프라인입니다. 최신 상태를 확인할 수 없습니다.", { exact: true }).first()).toBeVisible();
      if (feature === "rooms") await expect(page.getByRole("switch")).toBeDisabled();
      // 재연결 자체가 query를 재개하므로 연결을 열기 전에 응답 상태를 준비합니다.
      state.readMode = "empty"; await context.setOffline(false); await page.evaluate(() => window.contract.refetchEditors());
      if (feature === "streams") await expect(page.getByText("No live streams currently.", { exact: true })).toBeVisible();
      if (feature === "calendar") await expect(page.getByText("이 달에 등록된 기념일이 없습니다.", { exact: true })).toBeVisible();
      if (feature === "rooms") await expect(page.getByText("관리할 채팅방이 없습니다.", { exact: true })).toBeVisible();
    }
    state.readMode = "ok";
    await page.evaluate(() => window.contract.navigate("/dashboard/streams-view"));
    await page.getByRole("combobox", { name: "Select Stream Org" }).selectOption("vspo");
    await expect(page.getByText("vspo 한글 방송", { exact: true }).first()).toBeVisible();
    assert.equal(await page.getByText("hololive 한글 방송", { exact: true }).count(), 0);
    await page.evaluate(() => window.contract.navigate("/dashboard/calendar-view"));
    await page.evaluate(() => window.contract.refetchEditors());
    const before = new URLSearchParams(state.calls.filter(call => call.operation === "calendar").at(-1).query);
    for (let step = 0; step < 12; step++) await page.getByRole("button", { name: "다음 월", exact: true }).click();
    await expect.poll(() => {
      const last = new URLSearchParams(state.calls.filter(call => call.operation === "calendar").at(-1).query);
      return Number(last.get("year")) === Number(before.get("year")) + 1 && last.get("month") === before.get("month");
    }).toBe(true);
    await page.getByRole("button", { name: "이전 월", exact: true }).click();
    await page.getByRole("button", { name: "오늘", exact: true }).click();
    await page.evaluate(() => window.contract.navigate("/dashboard/rooms-view"));
    await page.evaluate(() => window.contract.refetchEditors());
    await page.getByRole("switch").click();
    await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "false");
    await expect(page.getByRole("switch")).toBeEnabled(); await page.getByRole("switch").click();
    await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "true");
    await page.getByRole("radio", { name: "화이트리스트", exact: true }).click();
    await expect(page.getByRole("radio", { name: "화이트리스트", exact: true })).toHaveAttribute("aria-checked", "true");
    await page.getByLabel("목록에 없는 채팅방 ID 직접 등록").fill("9007199254740995");
    await page.getByRole("button", { name: "허용 등록", exact: true }).click();
    await expect.poll(() => state.rooms.includes("9007199254740995")).toBe(true);
    assert.equal(state.readPagePosts, 4, "ACL actions and room addition each execute once");
  } finally { await context.close(); }
}


async function exerciseModuleRecovery(browser, base, state) {
  Object.assign(state, newState());
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await ready(page, base + "/__session_test?tab=MODULE"); await login(page);
    await page.evaluate(() => window.contract.navigate("/dashboard/module-probe"));
    const reload = page.getByRole("button", {name: "페이지 새로고침", exact: true});
    await expect(reload).toBeVisible();
    await page.evaluate(() => sessionStorage.setItem("fixture-module-ready", "yes"));
    await Promise.all([page.waitForEvent("load"), reload.click()]);
    await page.waitForFunction(() => window.contract?.ready);
    await page.evaluate(() => window.contract.navigate("/dashboard/module-probe"));
    await expect(page.getByText("화면 코드 복구 완료", {exact: true})).toBeVisible();
  } finally { await context.close(); }
}

async function exerciseLayout(browser, base, state) {
  Object.assign(state, newState(), { readPages: true, editors: true, docker: true, memberCount: 120 });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } }); const page = await context.newPage();
  try {
    await ready(page, base + "/__session_test?tab=LAYOUT"); await login(page);
    await page.evaluate(() => window.contract.navigate("/dashboard/stats"));
    const menu = page.getByRole("button", { name: "메뉴 열기", exact: true });
    for (const path of ["stats", "streams", "members", "calendar", "alarms", "rooms", "settings"]) {
      await menu.click();
      const drawer = page.getByRole("dialog", { name: "주 내비게이션" });
      const links = drawer.locator("nav a[href]");
      await expect(links.first()).toBeFocused();
      await page.keyboard.press("Shift+Tab");
      assert.equal(await drawer.evaluate(element => element.contains(document.activeElement)), true, "drawer keyboard focus cannot leave the modal");
      await drawer.locator('a[href="/dashboard/' + path + '"]').click();
      await expect.poll(() => page.evaluate(() => window.contract.currentRoute())).toBe("/dashboard/" + path);
      await expect(drawer).toHaveCount(0);
      await expect(menu).toBeFocused();
    }
    await page.evaluate(() => window.contract.navigate("/dashboard/members"));
    await expect(page.getByRole("button", { name: "한글 멤버 000 이름 수정", exact: true })).toBeVisible();
    const list = page.getByRole("list").first();
    const editName = page.getByRole("button", { name: "한글 멤버 047 이름 수정", exact: true });
    await list.scrollIntoViewIfNeeded();
    // overscan 행은 화면 밖에서도 isVisible()이 참입니다. 실제 노출과 측정이 끝난 뒤 한 번 클릭합니다.
    await expect(async () => {
      await list.evaluate(element => { element.scrollTop = element.scrollHeight; });
      await expect(editName).toBeInViewport({ ratio: 1, timeout: 250 });
    }).toPass({ timeout: 5000 });
    // 준비한 가상 목록 위치를 action 직전의 자동 scroll로 다시 바꾸지 않습니다.
    // 화면 노출·안정성·활성화·hit target 검사는 Playwright가 그대로 수행합니다.
    await editName.hover({ scroll: "none" });
    await editName.click({ scroll: "none" });
    const dialog = page.getByRole("dialog", { name: "멤버 이름 수정" });
    await expect(dialog).toContainText("9007199254741040");
    await expect(dialog.getByLabel("새로운 이름")).toHaveValue("한글 멤버 047");
    await dialog.getByRole("button", { name: "취소", exact: true }).click();
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.evaluate(() => { document.documentElement.style.fontSize = "200%"; });
    await page.getByRole("button", { name: "새로운 멤버 추가", exact: true }).click();
    const add = page.getByRole("dialog", { name: "새 멤버 추가" });
    await expect(add).toBeVisible();
    await page.screenshot({ path: "/tmp/hololive-admin-t07-text200-" + browser.browserType().name() + ".png" });
    const box = await add.boundingBox();
    assert(box && box.x >= 0 && box.y >= 0 && box.x + box.width <= 1281 && box.y + box.height <= 901, "200% text layout keeps the dialog inside the viewport");
    await expect(add.getByRole("button", { name: "취소", exact: true })).toBeVisible();
    await add.getByRole("button", { name: "취소", exact: true }).click();
  } finally { await context.close(); }
}

if (process.env.ADMIN_SESSION_BROWSER_CGROUP !== "1") {
  // CI에서 11개 시나리오가 엔진별 56~65초 이상 걸립니다. 개별 assertion 상한은 유지하고
  // 전체는 엔진별 120초, cgroup은 세 엔진과 정리 15초, 바깥 대기는 각각 15초 여유를 둡니다.
  test("session and generation browser contracts run in a bounded transient cgroup", { timeout: 405000 }, async () => {
    const unit = `iris-admin-session-contract-${process.pid}-${Date.now()}`;
    try {
      const selection = process.env.ADMIN_BROWSER_ENGINE ? [`--setenv=ADMIN_BROWSER_ENGINE=${process.env.ADMIN_BROWSER_ENGINE}`] : [];
      for (const name of ["LD_LIBRARY_PATH", "GST_PLUGIN_PATH"]) if (process.env[name]) selection.push(`--setenv=${name}=${process.env[name]}`);
      // Playwright의 dlopen 검사는 ldconfig cache를 읽으므로 테스트 namespace 안에서만 사설 cache를 제공합니다.
      const namespace = process.env.ADMIN_TEST_LDCACHE ? ["bwrap", "--bind", "/", "/", "--dev", "/dev", "--ro-bind", process.env.ADMIN_TEST_LDCACHE, "/etc/ld.so.cache"] : [];
      const result = await run("systemd-run", ["--user", "--quiet", "--wait", "--pipe", "--collect", `--unit=${unit}`, "--property=RuntimeMaxSec=375", "--property=KillMode=control-group", "--property=TimeoutStopSec=5", `--working-directory=${root}`, "--setenv=ADMIN_SESSION_BROWSER_CGROUP=1", ...selection, ...namespace, process.execPath, "--test", fileURLToPath(import.meta.url)], { timeout: 390000, maxBuffer: 2 ** 20 });
      process.stdout.write(result.stdout);
      process.stderr.write(result.stderr);
    } finally { await run("systemctl", ["--user", "stop", unit], { timeout: 10000 }).catch(() => undefined); }
  });
} else {
  for (const type of [chromium, firefox, webkit].filter(browser => !process.env.ADMIN_BROWSER_ENGINE || browser.name() === process.env.ADMIN_BROWSER_ENGINE)) test(`${type.name()}: shared cookies, stale responses, heartbeat and metadata fences`, { timeout: 120000 }, async t => {
    const state = newState();
    const server = await createServer({ root, configFile: path.join(root, "vite.config.ts"), logLevel: "error", plugins: [fixture(state)], server: { host: "127.0.0.1", port: 0, proxy: { "/admin/api": { target: "http://127.0.0.1:1", ws: false } } } });
    // pinned Playwright 1.63.0이 이미 제공하는 WS server를 사용하며 새 의존성을 추가하지 않습니다.
    const streams = new wsServer({ noServer: true, handleProtocols: protocols => state.wsMode === "ok" && protocols.has(`admin-stats.${CLIENT_GENERATION}`) ? `admin-stats.${CLIENT_GENERATION}` : false });
    server.httpServer.on("upgrade", (request, socket, head) => {
      if (request.url !== "/admin/api/ws/system-stats") return;
      state.wsRequests.push(request.headers["sec-websocket-protocol"]);
      streams.handleUpgrade(request, socket, head, peer => {
        state.sockets.add(peer);
        peer.on("error", () => {});
        peer.on("message", () => { state.clientFrames++; });
        peer.on("close", () => { state.sockets.delete(peer); });
        peer.send(JSON.stringify({ sequence: state.wsRequests.length }));
      });
    });
    let browser;
    try {
      await server.listen();
      const base = `http://127.0.0.1:${server.httpServer.address().port}`;
      const options = { headless: true };
      if (type === chromium) { await access("/usr/bin/google-chrome"); options.executablePath = "/usr/bin/google-chrome"; }
      browser = await type.launch(options);
      for (const exercise of [exerciseCookies, exerciseValidationPreparation, exerciseWarningLoading,
        exerciseStreams, exerciseAlarmControls, exerciseBusinessMutations, exerciseEditors,
        exerciseDocker, exerciseReadPages, exerciseModuleRecovery, exerciseLayout, exerciseMetadata]) {
        t.diagnostic(`${exercise.name}: started`);
        const started = performance.now();
        await exercise(browser, base, state);
        t.diagnostic(`${exercise.name}: ${Math.round(performance.now() - started)}ms`);
      }
    } finally { state.release?.(); for (const socket of streams.clients) socket.terminate(); streams.close(); await browser?.close(); await server.close(); }
  });
}
