import assert from "node:assert/strict";
import test from "node:test";
import { AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from "axios";
import { createAdminClient } from "@/api/client";
import { GenerationError, RequestBlockedError, SessionChangedError } from "@/api/errors";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { GenerationGate } from "@/app/generation";
import { SessionController, type SessionFact } from "@/session/controller";
import { SessionState } from "@/session/state";

const sessionBody = (token = "fixture-csrf") => ({ status: "ok", authenticated: true, username: "admin", absolute_expires_at: 2000000000, csrf_token: token,
	session_policy: { heartbeat_interval_ms: 300000, idle_timeout_ms: 600000, idle_warning_timeout_ms: 540000, idle_session_ttl_ms: 60000, absolute_warning_window_ms: 60000 } });
type Reply = { status?: number; data: unknown; generation?: string | null };

function fixture(handle: (config: InternalAxiosRequestConfig) => Reply | Promise<Reply>) {
	const state = new SessionState();
	const calls: InternalAxiosRequestConfig[] = [];
	const facts: SessionFact[] = [];
	const gate = new GenerationGate(() => controller.clearLocal());
	const { http, sdk } = createAdminClient("http://fixture.invalid", 1000, {
		execute(_operation, work) { return work(() => {}); },
		context: state.snapshot, beforeRequest(operation) { if (operation.access !== "public_metadata") gate.assertReady(); },
		onUnauthorized(epoch) { controller.unauthorized(epoch); }, onGenerationError: gate.block,
	});
	http.defaults.adapter = async (config): Promise<AxiosResponse<unknown>> => {
		calls.push(config);
		const response = await handle(config);
		const headers = new AxiosHeaders({ "Content-Type": "application/json" });
		if (response.generation !== null) headers.set("X-Admin-Server-Generation", response.generation ?? CLIENT_GENERATION);
		return { status: response.status ?? 200, statusText: "fixture", headers, data: response.data, config };
	};
	// 순서/세대 단위 시험의 주입 lock입니다. 실제 탭 간 cookie 순서는 browser 시험에서 확인합니다.
	const controller = new SessionController(sdk, state, { async run(_kind, work) { return work(); } }, {
		boundary() {}, changed(fact) { facts.push(fact); },
	});
	return { controller, state, calls, sdk, gate, facts };
}

test("generation gate prevents dispatch before metadata and stops all later work on missing or different generation", async () => {
	for (const header of [null, "older-generation"]) {
		const f = fixture(() => ({ data: { status: "ok" }, generation: header }));
		await assert.rejects(f.sdk.holoGetMembers(), error => error instanceof RequestBlockedError && !error.dispatched);
		assert.equal(f.calls.length, 0);
		f.gate.ready();
		await assert.rejects(f.sdk.holoGetMembers(), error => error instanceof GenerationError && error.reason === (header === null ? "missing" : "different"));
		assert.equal(f.gate.snapshot().phase, "incompatible");
		f.gate.ready();
		await assert.rejects(f.sdk.holoGetMembers(), RequestBlockedError);
		assert.equal(f.calls.length, 1);
	}
});

test("delayed previous 401 cannot clear a newer login or its CSRF token", async () => {
	const held = Promise.withResolvers<Reply>();
	const f = fixture(config => config.url?.endsWith("/members") ? held.promise :
		config.url?.endsWith("/login") ? { data: { status: "ok", message: "Logged in", csrf_token: "new-token" } } : { data: sessionBody("new-token") });
	f.gate.ready();
	const old = f.sdk.holoGetMembers();
	await f.controller.login("admin", "synthetic-password");
	const current = f.state.snapshot();
	held.resolve({ status: 401, data: { code: "UNAUTHORIZED", message: "Unauthorized", requestId: "fixture-request" } });
	await assert.rejects(old);
	assert.deepEqual(f.state.snapshot(), current);
	assert.notEqual(f.state.snapshot().phase, "signed_out");
	assert.deepEqual(f.facts, ["login"]);
});

test("CSRF rotation does not change auth generation and waits for the authoritative session GET", async () => {
	const f = fixture(config => config.url?.endsWith("/heartbeat") ? { data: { status: "ok", rotated: true, csrf_token: "rotated-token", absolute_expires_at: 2000000000 } } : { data: sessionBody("current-token") });
	f.gate.ready();
	f.state.acceptCSRF("initial-token", f.state.snapshot());
	const before = f.state.snapshot();
	await f.controller.heartbeat();
	assert.equal(f.state.snapshot().authGeneration, before.authGeneration);
	assert(f.state.snapshot().csrfVersion > before.csrfVersion);
	assert.equal(f.state.snapshot().csrfToken, "current-token");
	assert.deepEqual(f.calls.map(call => call.url), ["/admin/api/auth/heartbeat", "/admin/api/auth/session"]);
	assert.deepEqual(f.facts, ["rotation"]);
});

test("late session data and caller cancellation do not update CSRF or authenticated UI", async () => {
	for (const invalidate of ["auth", "csrf", "abort"]) {
		const held = Promise.withResolvers<Reply>();
		const f = fixture(() => held.promise);
		f.gate.ready();
		const signal = new AbortController();
		const pending = f.controller.refresh(signal.signal);
		if (invalidate === "auth") f.state.boundary();
		if (invalidate === "csrf") f.state.clearCSRF();
		if (invalidate === "abort") signal.abort();
		held.resolve({ data: sessionBody("late-token") });
		await assert.rejects(pending, error => error instanceof Error && error.name === "AbortError");
		assert.equal(f.state.snapshot().csrfToken, null);
		assert.notEqual(f.state.snapshot().phase, "authenticated");
	}
});

test("logout distinguishes confirmed family revocation from uncertain responses and never replays", async () => {
	for (const response of [{ data: { status: "ok" } }, { data: {} }, { status: 503, data: { code: "SERVICE_UNAVAILABLE", message: "Unavailable", requestId: "fixture" } }]) {
		const f = fixture(() => response);
		f.gate.ready();
		f.state.acceptCSRF("fixture-csrf", f.state.snapshot());
		const result = await f.controller.logout();
		assert.equal(result.revocation, "status" in response.data && response.data.status === "ok" ? "confirmed" : "unknown");
		assert.equal(result.clientCleanup, "completed");
		assert.equal(f.calls.length, 1);
		assert.equal(f.state.snapshot().csrfToken, null);
	}
});

test("no CSRF means heartbeat has no dispatch and no fabricated failure", async () => {
	const f = fixture(() => ({ data: {} }));
	f.gate.ready();
	for (let i = 0; i < 3; i++) await assert.rejects(f.controller.heartbeat(), SessionChangedError);
	assert.equal(f.calls.length, 0);
	assert.equal(f.state.snapshot().phase, "pending");
});
