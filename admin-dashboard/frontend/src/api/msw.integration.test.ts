import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { http, HttpResponse } from "msw";
import { adminClient } from "@/api/adminClient";
import apiClient from "@/api/client";
import { authApi, dockerApi } from "@/api/core";
import { alarmsApi } from "@/features/alarms/api";
import { membersApi } from "@/features/members/api";
import { roomsApi } from "@/features/rooms/api";
import { settingsApi } from "@/features/settings/api";
import { statsApi } from "@/features/stats/api";
import { streamsApi } from "@/features/streams/api";
import { server } from "@/mocks/server";

const originalWindow = globalThis.window;
const originalDocument = globalThis.document;

before(() => {
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
	adminClient.instance.defaults.baseURL = "http://localhost:30190";
	server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
	server.resetHandlers();
	Object.defineProperty(globalThis, "document", {
		value: {
			cookie: "csrf_token=msw-token",
		},
		configurable: true,
	});
});

after(() => {
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
	const session = await authApi.getSession();

	assert.equal(session.status, "ok");
	assert.equal(session.authenticated, true);
	assert.equal(session.username, "admin");
	assert.equal(session.session_policy.heartbeat_interval_ms, 300000);
});

test("msw serves docker containers through the root api client", async () => {
	const response = await dockerApi.getContainers();

	assert.equal(response.status, "ok");
	assert.equal(response.containers.length, 2);
	assert.equal(response.containers[0]?.name, "hololive-admin-api");
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
			HttpResponse.json({
				status: "ok",
				message: "logged in",
				csrf_token: "login-body-token",
			}),
		),
		http.post("*/admin/api/docker/containers/:name/restart", ({ request }) => {
			assert.equal(request.headers.get("x-csrf-token"), "login-body-token");
			return HttpResponse.json({ status: "ok", message: "restarted" });
		}),
	);

	await authApi.login("admin", "password");
	const response = await dockerApi.restartContainer("hololive-admin-api");

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
