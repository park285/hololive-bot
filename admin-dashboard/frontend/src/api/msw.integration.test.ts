import { locks } from "node:worker_threads";
import { contractJSON } from "@/mocks/contract";
import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { http } from "msw";
import { httpClient as apiClient, session, generation, adminClient } from "@/app/bootstrap";
import { dockerApi } from "@/features/docker/api";
import { alarmsApi, namesApi } from "@/features/alarms/api";
import { membersApi } from "@/features/members/api";
import { roomsApi } from "@/features/rooms/api";
import { settingsApi } from "@/features/settings/api";
import { statsApi } from "@/features/stats/api";
import { streamsApi } from "@/features/streams/api";
import { server } from "@/mocks/server";

function seedCSRF(token: string): void { generation.ready(); session.state.acceptCSRF(token, session.state.snapshot()); }

const originalWindow = globalThis.window;
const originalDocument = globalThis.document;

const oldLocks = Object.getOwnPropertyDescriptor(navigator, "locks");
const oldOnline = Object.getOwnPropertyDescriptor(navigator, "onLine");
before(() => {
	Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
	Object.defineProperty(navigator, "locks", { value: locks, configurable: true });
	generation.ready();
	Object.defineProperty(globalThis, "window", {
		value: {
			location: {
				origin: "http://localhost:30190",
				pathname: "/dashboard",
				href: "http://localhost:30190/dashboard",
			},
			setTimeout,
			clearTimeout,
		},
		configurable: true,
	});
	Object.defineProperty(globalThis, "document", {
		value: {
			cookie: "csrf_token=msw-token",
		},
		configurable: true,
	});
	apiClient.defaults.baseURL = "http://localhost:30190/admin/api";
	server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
	seedCSRF("fixture-csrf");
	server.resetHandlers();
	Object.defineProperty(globalThis, "document", {
		value: {
			cookie: "csrf_token=msw-token",
		},
		configurable: true,
	});
});

after(() => {
	if (oldOnline) Object.defineProperty(navigator, "onLine", oldOnline); else Reflect.deleteProperty(navigator, "onLine");
	if (oldLocks) Object.defineProperty(navigator, "locks", oldLocks); else Reflect.deleteProperty(navigator, "locks");
	server.close();

	if (originalWindow === undefined) {
		Reflect.deleteProperty(globalThis, "window");
	} else {
		Object.defineProperty(globalThis, "window", {
			value: originalWindow,
			configurable: true,
		});
	}

	if (originalDocument === undefined) {
		Reflect.deleteProperty(globalThis, "document");
	} else {
		Object.defineProperty(globalThis, "document", {
			value: originalDocument,
			configurable: true,
		});
	}
});

test("msw serves session bootstrap through the root api client", async () => {
	const currentSession = await session.refresh();

	assert.equal(currentSession.status, "ok");
	assert.equal(currentSession.authenticated, true);
	assert.equal(currentSession.username, "admin");
	assert.equal(currentSession.session_policy.heartbeat_interval_ms, 300000);
});

test("msw serves docker containers through the root api client", async () => {
	const response = await dockerApi.getContainers();

	assert.equal(response.status, "ok");
	assert.equal(response.containers.length, 2);
	assert.equal(response.containers[0]?.name, "hololive-api");
});

test("auth login csrf token is reused for unsafe requests without readable cookie", async () => {
	Object.defineProperty(globalThis, "document", {
		value: {
			cookie: "",
		},
		configurable: true,
	});

	server.use(
		http.post("*/admin/api/auth/login", () =>
			contractJSON({
				status: "ok",
				message: "logged in",
				csrf_token: "msw-token",
			}),
		),
		http.post("*/admin/api/docker/containers/:name/restart", ({ request }) => {
			assert.equal(request.headers.get("x-csrf-token"), "msw-token");
			return contractJSON({ status: "ok", message: "restarted" });
		}),
	);

	await session.login("admin", "password");
	const response = await dockerApi.restartContainer("hololive-api");

	assert.equal(response.status, "ok");
});

test("msw serves settings updates through the generated admin client path", async () => {
	const response = await settingsApi.update({ alarmAdvanceMinutes: 12 });

	assert.equal(response.status, "ok");
	assert.equal(response.settings.alarmAdvanceMinutes, 12);
});

test("msw serves the stats summary query", async () => {
	const summary = await statsApi.get();

	assert.equal(summary.status, "ok");
	assert.equal(summary.members, 2);
});

test("msw serves generated-client collection endpoints", async () => {
	const [members, alarms, rooms] = await Promise.all([
		membersApi.getAll(),
		alarmsApi.getAll(),
		roomsApi.getAll(),
	]);

	assert.equal(members.status, "ok");
	assert.equal(members.members.length, 2);
	assert.equal(alarms.status, "ok");
	assert.equal(alarms.alarms.length, 1);
	assert.equal(rooms.status, "ok");
	assert.equal(rooms.rooms.length, 2);
});

test("roomsApi removes an ACL entry with a DELETE request body", async () => {
	server.use(
		http.delete("*/admin/api/holo/rooms", async ({ request }) => {
			assert.deepEqual(await request.json(), { room: "200000000000002" });
			return contractJSON({ status: "ok", message: "removed" });
		}),
	);

	const response = await roomsApi.remove({ room: "200000000000002" });

	assert.equal(response.status, "ok");
});

test("msw preserves stream query defaults and upcoming results", async () => {
	const [live, upcoming] = await Promise.all([
		streamsApi.getLive("hololive"),
		streamsApi.getUpcoming("hololive"),
	]);

	assert.equal(live.status, "ok");
	assert.equal(live.org, "hololive");
	assert.equal(live.streams.length, 1);
	assert.equal(upcoming.status, "ok");
	assert.equal(upcoming.streams.length, 1);
});

test("hidden user-name API preserves the full string ID and sends one validated mutation", async () => {
	let calls = 0;
	const id = "9007199254740993";
	server.use(http.post("*/admin/api/holo/names/user", async ({ request }) => {
		calls++;
		assert.deepEqual(await request.json(), { userId: id, userName: "한글 사용자" });
		assert.match(request.headers.get("x-admin-mutation-id") ?? "", /^[a-f0-9-]{36}$/);
		return contractJSON({ status: "ok" });
	}));
	assert.equal((await namesApi.setUserName(id, "한글 사용자")).status, "ok");
	assert.equal(calls, 1);
});

test("hidden community-shorts diagnostics remain available through the validated SDK", async () => {
	let calls = 0;
	const body = {
		status: "ok", generatedAt: "2026-09-09T00:00:00Z", windowStart: "2026-09-08T00:00:00Z", windowEnd: "2026-09-09T00:00:00Z", windowHours: 24, observedAtBasis: "detected_at", slaThresholdMillis: 30000,
		overview: { channelCount: 0, detectedPostCount: 0, alarmSentPostCount: 0, successPostCount: 0, failedPostCount: 0, detectedUnsentPostCount: 0, pendingPostCount: 0, latencyMeasuredPostCount: 0, withinTargetPostCount: 0, exceededPostCount: 0, communityDetectedPostCount: 0, shortsDetectedPostCount: 0, communityExceededPostCount: 0, shortsExceededPostCount: 0 }, channels: [],
	};
	server.use(http.get("*/admin/api/holo/stats/youtube/community-shorts", () => { calls++; return contractJSON(body); }));
	assert.deepEqual((await adminClient.holoGetYouTubeCommunityShortsOps()).data, body);
	assert.equal(calls, 1);
});
