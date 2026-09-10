import assert from "node:assert/strict";
import test from "node:test";
import axios, { AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from "axios";
import { Admin } from "@/api/generated/Admin";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { createSDKTransport } from "@/api/transport";
import { ContractError } from "@/api/errors";

function fixture(response: { body: unknown; status?: number; generation?: string | null; contentType?: string }) {
	const calls: InternalAxiosRequestConfig[] = [];
	const context = { authGeneration: 0, csrfToken: "fixture-csrf" };
	const client = axios.create({
		baseURL: "http://fixture.invalid/admin/api", timeout: 30000,
		adapter: async (config): Promise<AxiosResponse<unknown>> => {
			calls.push(config);
			const headers = new AxiosHeaders({ "Content-Type": response.contentType ?? "application/json" });
			if (response.generation !== null) headers.set("X-Admin-Server-Generation", response.generation ?? CLIENT_GENERATION);
			return { status: response.status ?? 200, statusText: "fixture", headers, data: response.body, config };
		},
	});
	return { calls, context, sdk: new Admin(createSDKTransport(client, {
		execute(_operation, work) { return work(() => {}); },
		context: () => ({ ...context }), beforeRequest() {}, onUnauthorized() {}, onGenerationError() {},
	})) };
}

test("SDK uses its injected transport once and preserves a large path ID and CSRF header", async () => {
	const { sdk, calls } = fixture({ body: { status: "ok" } });
	await sdk.holoUpdateMemberName("9007199254740993", { name: "한글" });
	assert.equal(calls.length, 1);
	assert.equal(calls[0]?.url, "/admin/api/holo/members/9007199254740993/name");
	assert.equal(calls[0]?.baseURL, "http://fixture.invalid");
	assert.equal(calls[0]?.headers.get("X-Admin-Client-Generation"), CLIENT_GENERATION);
	assert.equal(calls[0]?.headers.get("X-CSRF-Token"), "fixture-csrf");
});

test("a session boundary while preparing validators prevents both reads and mutations from reaching HTTP", async () => {
	for (const mutation of [false, true]) {
		const { sdk, calls, context } = fixture({ body: { status: "ok" } });
		const pending = mutation ? sdk.holoUpdateMemberName("1", { name: "한글" }) : sdk.holoGetMembers();
		context.authGeneration++;
		await assert.rejects(pending, { name: "AbortError" });
		assert.equal(calls.length, 0);
	}
});

test("cancellation before or during validator preparation sends no request", async () => {
	for (const alreadyAborted of [false, true]) {
		const { sdk, calls } = fixture({ body: { status: "ok" } });
		const controller = new AbortController();
		if (alreadyAborted) controller.abort();
		const pending = sdk.holoUpdateMemberName("1", { name: "한글" }, { signal: controller.signal });
		controller.abort();
		await assert.rejects(pending, { name: "AbortError" });
		assert.equal(calls.length, 0);
	}
});

test("validator preparation uses the current CSRF and deducts preparation time from the HTTP budget", async () => {
	const { sdk, calls, context } = fixture({ body: { status: "ok" } });
	const pending = sdk.holoUpdateMemberName("1", { name: "한글" });
	context.csrfToken = "rotated-csrf";
	await pending;
	assert.equal(calls[0]?.headers.get("X-CSRF-Token"), "rotated-csrf");
	assert((calls[0]?.timeout ?? 0) > 0 && (calls[0]?.timeout ?? Infinity) < 30000);
});

test("invalid input is rejected before the fake upstream receives anything", async () => {
	const { sdk, calls } = fixture({ body: { status: "ok" } });
	await assert.rejects(sdk.holoUpdateMemberName("not-an-id", { name: "한글" }), error => error instanceof ContractError && !error.dispatched);
	assert.equal(calls.length, 0);
});

test("malformed, HTML, missing-generation and mismatched-generation replies remain uncertain without replay", async () => {
	for (const response of [
		{ body: {} }, { body: "<html>unavailable</html>", contentType: "text/html" },
		{ body: { status: "ok" }, generation: null }, { body: { status: "ok" }, generation: "old" },
	]) {
		const { sdk, calls } = fixture(response);
		await assert.rejects(sdk.holoUpdateMemberName("1", { name: "한글" }), error => error instanceof ContractError && error.dispatched);
		assert.equal(calls.length, 1);
	}
});

test("upstream authentication error retains the BFF 502 boundary without another call", async () => {
	const { sdk, calls } = fixture({ body: { code: "UPSTREAM_AUTH_FAILED", message: "내부 인증 실패", requestId: "fixture-request" }, status: 502 });
	await assert.rejects(sdk.holoGetMembers(), error => axios.isAxiosError(error) && error.response?.status === 502);
	assert.equal(calls.length, 1);
});
