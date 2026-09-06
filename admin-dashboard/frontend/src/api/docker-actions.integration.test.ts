import assert from "node:assert/strict";
import { after, afterEach, before, test } from "node:test";
import { isAxiosError } from "axios";
import { http, HttpResponse } from "msw";
import apiClient, { clearCSRFToken, setCSRFToken } from "@/api/client";
import { dockerApi } from "@/api/core";
import { server } from "@/mocks/server";

const originalBaseURL = apiClient.defaults.baseURL;
const baseURL = "http://localhost:30190/admin/api";
const containerName = "test-worker";
const actions = [
	["restart", dockerApi.restartContainer],
	["stop", dockerApi.stopContainer],
	["start", dockerApi.startContainer],
] as const;

before(() => {
	apiClient.defaults.baseURL = baseURL;
	setCSRFToken("docker-action-test-token");
	server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
	server.resetHandlers();
});

after(() => {
	server.close();
	apiClient.defaults.baseURL = originalBaseURL;
	clearCSRFToken();
});

function isOutcomeUnknown(error: unknown): boolean {
	return error instanceof Error && error.name === "DockerActionOutcomeUnknownError";
}

for (const [action, invoke] of actions) {
	const url = `${baseURL}/docker/containers/${containerName}/${action}`;

	test(`docker ${action} requires a confirmed response and preserves CSRF`, async () => {
		let requests = 0;
		server.use(http.post(url, ({ request }) => {
			requests += 1;
			assert.equal(request.headers.get("x-csrf-token"), "docker-action-test-token");
			return HttpResponse.json({ status: "ok", message: "confirmed", extra: true });
		}));

		assert.deepEqual(await invoke(containerName), { status: "ok", message: "confirmed" });
		assert.equal(requests, 1);
	});

	test(`docker ${action} rejects malformed success bodies without another POST`, async (t) => {
		const cases = [
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
					return HttpResponse.json(body);
				}));
				await assert.rejects(invoke(containerName), isOutcomeUnknown);
				assert.equal(requests, 1);
			});
		}
	});

	test(`docker ${action} accepts an omitted or nullable optional message`, async (t) => {
		for (const body of [{ status: "ok" }, { status: "ok", message: null }]) {
			await t.test(JSON.stringify(body), async () => {
				server.use(http.post(url, () => HttpResponse.json(body)));
				assert.equal((await invoke(containerName)).status, "ok");
			});
		}
	});

	test(`docker ${action} does not invent success for an empty HTTP 200`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return new HttpResponse(null, { status: 200 });
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
						? new HttpResponse(null, { status })
						: HttpResponse.json({ status: "ok" }, { status });
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
			return HttpResponse.json({ error: "forbidden" }, { status: 403 });
		}));
		await assert.rejects(invoke(containerName), (error: unknown) =>
			isAxiosError(error) && error.response?.status === 403);
		assert.equal(requests, 1);
	});

	test(`docker ${action} reports server failure as uncertain without replay`, async () => {
		let requests = 0;
		server.use(http.post(url, () => {
			requests += 1;
			return HttpResponse.json({ error: "upstream unavailable" }, { status: 502 });
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
			return HttpResponse.json({ status: "ok" });
		}));
		apiClient.defaults.timeout = 500;

		await assert.rejects(invoke(containerName), (error: unknown) =>
			isOutcomeUnknown(error) && error instanceof Error &&
			isAxiosError(error.cause) &&
			(error.cause.code === "ECONNABORTED" || error.cause.code === "ETIMEDOUT"));
		assert.equal(requests, 1);
	});
}
