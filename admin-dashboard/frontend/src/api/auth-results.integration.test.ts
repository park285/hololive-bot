import { locks } from "node:worker_threads";
import { contractJSON } from "@/mocks/contract";
import assert from "node:assert/strict";
import { after, afterEach, before, beforeEach, test } from "node:test";
import { http } from "msw";
import { httpClient as apiClient, session, generation } from "@/app/bootstrap";
import { server } from "@/mocks/server";

function seedCSRF(token: string): void { generation.ready(); session.state.acceptCSRF(token, session.state.snapshot()); }

const previousBaseURL = apiClient.defaults.baseURL;
const baseURL = "http://localhost:30190/admin/api";
const sessionResponse = {
	status: "ok", authenticated: true, username: "admin", csrf_token: "authoritative-token", absolute_expires_at: 2000000000,
	session_policy: { heartbeat_interval_ms: 300000, idle_timeout_ms: 600000, idle_warning_timeout_ms: 30000, idle_session_ttl_ms: 60000, absolute_warning_window_ms: 60000 },
};

const oldLocks = Object.getOwnPropertyDescriptor(navigator, "locks");
const oldOnline = Object.getOwnPropertyDescriptor(navigator, "onLine");
before(() => {
	Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
	Object.defineProperty(navigator, "locks", { value: locks, configurable: true });
	generation.ready();
	apiClient.defaults.baseURL = baseURL;
	server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => { seedCSRF("fixture-csrf"); });
afterEach(() => { server.resetHandlers(); session.state.clearCSRF(); });
after(() => {
	if (oldOnline) Object.defineProperty(navigator, "onLine", oldOnline); else Reflect.deleteProperty(navigator, "onLine");
	if (oldLocks) Object.defineProperty(navigator, "locks", oldLocks); else Reflect.deleteProperty(navigator, "locks"); server.close(); apiClient.defaults.baseURL = previousBaseURL; });

test("logout requires confirmed revocation and never repeats its POST", async (t) => {
	for (const [status, body] of [[200, { status: "ok" }], [200, {}], [200, null], [200, "<html>"], [503, { error: "Session store unavailable" }]] as const) {
		await t.test(`${status} ${JSON.stringify(body)}`, async () => {
			let posts = 0;
			seedCSRF("logout-contract-csrf");
			server.use(http.post(`${baseURL}/auth/logout`, ({ request }) => {
				posts += 1;
				assert.equal(request.headers.get("x-csrf-token"), "logout-contract-csrf");
				return contractJSON(body, { status });
			}));
			if (status === 200 && typeof body === "object" && body !== null && "status" in body) {
				assert.equal((await session.logout()).revocation, "confirmed");
			} else {
				assert.equal((await session.logout()).revocation, "unknown");
			}
			assert.equal(posts, 1);
		});
	}
});

test("heartbeat preserves known success and idle shapes", async () => {
	let sessionReads = 0;
	server.use(http.get(`${baseURL}/auth/session`, () => { sessionReads += 1; return contractJSON(sessionResponse); }));
	for (const body of [{ status: "ok", absolute_expires_at: 2000000000 }, { status: "idle", idle_rejected: true }, { status: "ok", absolute_expires_at: 2000000000, rotated: true, csrf_token: "new-token" }]) {
		server.use(http.post(`${baseURL}/auth/heartbeat`, () => contractJSON(body)));
		const response = await session.heartbeat();
		assert.equal(response.status, body.status);
		if (body.rotated) assert.equal(session.state.snapshot().csrfToken, "authoritative-token");
	}
	assert.equal(sessionReads, 1, "rotation must read the current shared-cookie session");
});

test("scheduled heartbeats wait for the authoritative CSRF state without dispatch or failure escalation", async () => {
	let posts = 0;
	server.use(http.post(`${baseURL}/auth/heartbeat`, () => {
		posts++;
		return contractJSON({ status: "ok", absolute_expires_at: 2000000000 });
	}));
	session.state.clearCSRF();
	for (let i = 0; i < 3; i++) await assert.rejects(session.heartbeat(), (error: unknown) => error instanceof Error && error.name === "AbortError");
	assert.equal(posts, 0);
	server.use(http.get(`${baseURL}/auth/session`, () => contractJSON(sessionResponse)));
	await session.refresh();
	await session.heartbeat();
	assert.equal(posts, 1);
});

test("heartbeat rejects HTTP failures and malformed shapes without installing a token", async (t) => {
	for (const [status, body] of [[503, { error: "unavailable" }], [502, "<html>error</html>"], [200, {}], [200, null], [200, []], [200, { status: "ok" }], [200, { status: "idle" }], [200, { status: "ok", absolute_expires_at: "invalid", csrf_token: "untrusted" }]] as const) {
		await t.test(`${status} ${JSON.stringify(body)}`, async () => {
			let posts = 0;
			server.use(http.post(`${baseURL}/auth/heartbeat`, () => {
				posts += 1;
				return contractJSON(body, { status });
			}));
			await assert.rejects(session.heartbeat());
			assert.equal(posts, 1);
		});
	}
});

test("a late session GET cannot overwrite a newer rotation or logout", async () => {
	let release!: () => void;
	let entered!: () => void;
	const held = new Promise<void>((resolve) => { release = resolve; });
	const started = new Promise<void>((resolve) => { entered = resolve; });
	server.use(http.get(`${baseURL}/auth/session`, async () => { entered(); await held; return contractJSON(sessionResponse); }));
	const result = session.refresh();
	await started;
	session.state.clearCSRF();
	seedCSRF("newer-token");
	release();
	await assert.rejects(result, (error: unknown) => error instanceof Error && error.name === "AbortError");
	assert.equal(session.state.snapshot().csrfToken, "newer-token");
});

test("a canceled bootstrap GET cannot install CSRF before the active GET completes", async () => {
	let release!: () => void;
	let entered!: () => void;
	const held = new Promise<void>((resolve) => { release = resolve; });
	const started = new Promise<void>((resolve) => { entered = resolve; });
	let reads = 0;
	server.use(http.get(`${baseURL}/auth/session`, async () => {
		reads++;
		if (reads === 1) { entered(); await held; }
		return contractJSON(sessionResponse);
	}));
	const controller = new AbortController();
	const canceled = session.refresh(controller.signal);
	const rejection = assert.rejects(canceled);
	await started;
	controller.abort();
	release();
	await rejection;
	assert.equal((await session.refresh()).csrf_token, "authoritative-token");
});
