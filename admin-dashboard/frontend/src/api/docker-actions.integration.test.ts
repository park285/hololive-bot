import { contractJSON } from "@/mocks/contract";
import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { isAxiosError } from "axios";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { http, HttpResponse } from "msw";
import { httpClient as apiClient, session, generation, operations } from "@/app/bootstrap";
import { dockerApi } from "@/features/docker/api";
import { server } from "@/mocks/server";

function seedCSRF(token: string): void { generation.ready(); session.state.acceptCSRF(token, session.state.snapshot()); }

const originalBaseURL = apiClient.defaults.baseURL;
const baseURL = "http://localhost:30190/admin/api";
const containerName = "hololive-api";
const actions = [
	["restart", dockerApi.restartContainer],
	["stop", dockerApi.stopContainer],
	["start", dockerApi.startContainer],
] as const;

const oldOnline = Object.getOwnPropertyDescriptor(navigator, "onLine");
before(() => {
	Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
	apiClient.defaults.baseURL = baseURL;
	seedCSRF("docker-action-test-token");
	server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
	server.resetHandlers();
});

after(() => {
	if (oldOnline) Object.defineProperty(navigator, "onLine", oldOnline); else Reflect.deleteProperty(navigator, "onLine");
	server.close();
	apiClient.defaults.baseURL = originalBaseURL;
	session.state.clearCSRF();
});

function isOutcomeUnknown(error: unknown): boolean {
	return operations.failure(error)?.kind === "unknown";
}

for (const [action, invoke] of actions) {
	const url = `${baseURL}/docker/containers/${containerName}/${action}`;

	test(`docker ${action} requires a confirmed response and preserves CSRF`, async () => {
		let requests = 0;
		server.use(http.post(url, ({ request }) => {
			requests += 1;
			assert.equal(request.headers.get("x-csrf-token"), "docker-action-test-token");
			return contractJSON({ status: "ok", message: "confirmed" });
		}));

		assert.deepEqual(await invoke(containerName), { status: "ok", message: "confirmed" });
		assert.equal(requests, 1);
	});

	test(`docker ${action} rejects malformed success bodies without another POST`, async (t) => {
		const cases = [
			["unknown field", { status: "ok", extra: true }],
			["empty object", {}],
			["null", null],
			["array", []],
			["string", "ok"],
			["missing status", { message: "unconfirmed" }],
			["null status", { status: null }],
			["wrong status", { status: "error" }],
			["wrong status type", { status: true }],
			["wrong message type", { status: "ok", message: { secret: "must-not-be-logged" } }],
		] as const;

		for (const [name, body] of cases) {
			await t.test(name, async () => {
				let requests = 0;
				server.use(http.post(url, () => {
					requests += 1;
					return contractJSON(body);
				}));
				await assert.rejects(invoke(containerName), isOutcomeUnknown);
				assert.equal(requests, 1);
			});
		}
	});

	test(`docker ${action} accepts an omitted or nullable optional message`, async (t) => {
		for (const body of [{ status: "ok" }, { status: "ok", message: null }]) {
			await t.test(JSON.stringify(body), async () => {
				server.use(http.post(url, () => contractJSON(body)));
				assert.equal((await invoke(containerName)).status, "ok");
			});
		}
	});

	test(`docker ${action} does not invent success for an empty HTTP 200`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return new HttpResponse(null, { status: 200, headers: { "X-Admin-Server-Generation": CLIENT_GENERATION, "Content-Type": "application/json" } });
		}));
		await assert.rejects(invoke(containerName), isOutcomeUnknown);
		assert.equal(requests, 1);
	});

	test(`docker ${action} does not treat asynchronous or empty responses as completion`, async (t) => {
		for (const status of [202, 204]) {
			await t.test(String(status), async () => {
				let requests = 0;
				server.use(http.post(url, () => {
					requests += 1;
					return status === 204
						? new HttpResponse(null, { status, headers: { "X-Admin-Server-Generation": CLIENT_GENERATION } })
						: contractJSON({ status: "ok" }, { status });
				}));
				await assert.rejects(invoke(containerName), isOutcomeUnknown);
				assert.equal(requests, 1);
			});
		}
	});

	test(`docker ${action} preserves explicit authorization refusal`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return contractJSON({ code: "FORBIDDEN", message: "forbidden", requestId: "fixture-request" }, { status: 403 });
		}));
		await assert.rejects(invoke(containerName), (error: unknown) =>
			isAxiosError(error) && error.response?.status === 403);
		assert.equal(requests, 1);
	});

	test(`docker ${action} reports server failure as uncertain without replay`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return contractJSON({ code: "UPSTREAM_UNAVAILABLE", message: "upstream unavailable", requestId: "fixture-request" }, { status: 502 });
		}));
		await assert.rejects(invoke(containerName), isOutcomeUnknown);
		assert.equal(requests, 1);
	});

	test(`docker ${action} preserves uncertain outcome after dispatch and connection loss`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return HttpResponse.error();
		}));
		await assert.rejects(invoke(containerName), (error: unknown) =>
			isOutcomeUnknown(error) && error instanceof Error && error.cause instanceof Error);
		assert.equal(requests, 1);
	});

	test(`docker ${action} preserves timeout cause without replay`, { timeout: 5_000 }, async (t) => {
		const originalTimeout = apiClient.defaults.timeout;
		let releaseResponse: (() => void) | undefined;
		const responseGate = new Promise<void>((resolve) => {
			releaseResponse = resolve;
		});
		t.after(() => {
			apiClient.defaults.timeout = originalTimeout;
			releaseResponse?.();
		});

		let requests = 0;
		server.use(http.post(url, async () => {
			requests += 1;
			await responseGate;
			return contractJSON({ status: "ok" });
		}));
		apiClient.defaults.timeout = 500;

		await assert.rejects(invoke(containerName), (error: unknown) =>
			isOutcomeUnknown(error) && isAxiosError(error) &&
			(error.code === "ECONNABORTED" || error.code === "ETIMEDOUT"));
		assert.equal(requests, 1);
	});
}
