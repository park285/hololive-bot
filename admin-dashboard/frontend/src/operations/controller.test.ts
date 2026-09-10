import assert from "node:assert/strict";
import test from "node:test";
import { AxiosError, AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from "axios";
import { createAdminClient } from "@/api/client";
import { ContractError, RequestBlockedError } from "@/api/errors";
import { CLIENT_GENERATION } from "@/api/generated/generation";
import { Operations } from "@/operations/controller";

type Reply = { status?: number; data: unknown };
function fixture(handle: (config: InternalAxiosRequestConfig) => Reply | Promise<Reply>) {
	const state = { epoch: 0, online: true };
	const calls: InternalAxiosRequestConfig[] = [];
	const operations = new Operations(() => state.epoch, () => state.online);
	const { http, sdk } = createAdminClient("http://fixture.invalid", 1000, {
		context: () => ({ authGeneration: state.epoch, csrfToken: "fixture-csrf" }),
		execute(operation, work) { return operation.mutation ? operations.execute(operation, work) : work(() => {}); },
		beforeRequest() {}, onUnauthorized() {}, onGenerationError() {},
	});
	http.defaults.adapter = async (config): Promise<AxiosResponse<unknown>> => {
		calls.push(config);
		const response = await handle(config);
		return { status: response.status ?? 200, statusText: "fixture", headers: new AxiosHeaders({ "Content-Type": "application/json", "X-Admin-Server-Generation": CLIENT_GENERATION }), data: response.data, config };
	};
	return { state, calls, operations, sdk };
}

test("one application business mutation rejects BUSY without queuing while authentication stays independent", async () => {
	const held = Promise.withResolvers<Reply>();
	const f = fixture(config => config.url?.endsWith("/name") ? held.promise : config.url?.endsWith("/heartbeat") ? { data: { status: "idle", idle_rejected: true } } : { data: { status: "ok" } });
	const pending = f.sdk.holoUpdateMemberName("9007199254740993", { name: "한글" });
	assert.equal(f.operations.snapshot().busy, true);
	await assert.rejects(f.sdk.holoAddRoom({ room: "1" }), error => error instanceof RequestBlockedError && error.code === "BUSY" && !error.dispatched);
	await f.sdk.handleHeartbeat({ idle: true });
	await f.sdk.handleLogout();
	assert.equal(f.calls.length, 3);
	held.resolve({ data: { status: "ok" } });
	await pending;
	assert.equal(f.calls.length, 3, "BUSY never resumes after release");
	assert.equal(f.operations.snapshot().busy, false);
	assert.equal(f.operations.snapshot().result?.outcome.kind, "succeeded");
});

test("offline submission and invalid input have zero dispatch and never replay after reconnection", async () => {
	const f = fixture(() => ({ data: { status: "ok" } }));
	f.state.online = false;
	await assert.rejects(f.sdk.holoAddRoom({ room: "1" }), error => error instanceof RequestBlockedError && error.code === "OFFLINE");
	assert.equal(f.operations.snapshot().result?.outcome.kind, "rejected");
	f.state.online = true;
	await Promise.resolve();
	await assert.rejects(f.sdk.holoUpdateMemberName("invalid", { name: "한글" }), error => error instanceof ContractError && !error.dispatched);
	assert.equal(f.calls.length, 0);
});

test("settings save, application and propagation effects remain separate after a failed read", async () => {
	const f = fixture(config => config.method === "post" ? { data: { status: "ok", message: "Saved", settings: { alarmAdvanceMinutes: 15 }, runtime: { alarm_applied: true, config_publish_alarm_advance_minutes: false } } } : { data: {} });
	await f.sdk.holoUpdateSettings({ alarmAdvanceMinutes: 15 });
	const result = f.operations.snapshot().result;
	assert.equal(result?.outcome.kind, "partial");
	assert.deepEqual(result?.outcome.effects.map(effect => effect.state), ["confirmed", "confirmed", "failed"]);
	await assert.rejects(f.sdk.holoGetSettings());
	assert.equal(f.operations.snapshot().result, result, "refresh failure cannot rewrite a confirmed save");
});

test("a missing application or propagation flag stays unknown rather than becoming false or success", async () => {
	const f = fixture(() => ({ data: { status: "ok", message: "Saved", settings: { alarmAdvanceMinutes: 0 }, runtime: {} } }));
	await f.sdk.holoUpdateSettings({ alarmAdvanceMinutes: 0 });
	assert.deepEqual(f.operations.snapshot().result?.outcome.effects.map(effect => effect.state), ["confirmed", "unknown", "unknown"]);
});

test("post-dispatch network loss, malformed data and unsupported acceptance remain unknown without replay", async () => {
	for (const mode of ["network", "malformed", "accepted", "tracking-without-contract"]) {
		const f = fixture(() => {
			if (mode === "network") throw new AxiosError("Connection lost", "ERR_NETWORK");
			return mode === "malformed" ? { data: {} } : { status: 202, data: mode === "accepted" ? { status: "ok" } : { status: "ok", trackingId: "synthetic" } };
		});
		await assert.rejects(f.sdk.holoAddRoom({ room: "1" }));
		assert.equal(f.calls.length, 1);
		assert.equal(f.operations.snapshot().result?.outcome.kind, "unknown");
	}
});

test("only matching first-claim evidence proves a pre-dispatch rejection", async () => {
	for (const [proof, status, expected] of [["matching", 403, "rejected"], ["missing", 403, "unknown"], ["wrong", 403, "unknown"], ["matching", 503, "failed"]] as const) {
		const f = fixture(config => ({ status, data: { code: status === 403 ? "FORBIDDEN" : "SERVICE_UNAVAILABLE", message: "Rejected", requestId: "correlation-only", ...(proof === "missing" ? {} : { notDispatchedMutationId: proof === "matching" ? config.headers.get("X-Admin-Mutation-ID") : "5ae58f70-51c4-4df2-bf70-683773e40638" }) } }));
		await assert.rejects(f.sdk.holoAddRoom({ room: "1" }));
		assert.equal(f.operations.snapshot().result?.outcome.kind, expected);
		assert.equal(f.calls.length, 1);
	}
});

test("a mismatched rejection status and a duplicate mutation cannot prove absence of an effect", async () => {
	for (const [status, code] of [[502, "FORBIDDEN"], [409, "MUTATION_ALREADY_ATTEMPTED"], [503, "ADMISSION_CLOSED"], [503, "MUTATION_ADMISSION_UNAVAILABLE"]] as const) {
		const f = fixture(() => ({ status, data: { code, message: "Uncertain", requestId: "correlation-only" } }));
		await assert.rejects(f.sdk.holoAddRoom({ room: "1" }));
		assert.equal(f.operations.snapshot().result?.outcome.kind, "unknown");
		assert.equal(f.calls.length, 1);
	}
});

test("session boundary hides sensitive results while retaining the pending request lock", async () => {
	const held = Promise.withResolvers<Reply>();
	const dispatched = Promise.withResolvers<void>();
	const f = fixture(() => { dispatched.resolve(); return held.promise; });
	const pending = f.sdk.holoAddRoom({ room: "1" });
	await dispatched.promise;
	f.state.epoch++;
	f.operations.hideSensitive();
	assert.deepEqual(f.operations.snapshot(), { busy: true, activeLabel: null, result: null });
	await assert.rejects(f.sdk.holoAddRoom({ room: "2" }), RequestBlockedError);
	held.resolve({ data: { status: "ok" } });
	await pending;
	assert.deepEqual(f.operations.snapshot(), { busy: false, activeLabel: null, result: null });
	assert.equal(f.calls.length, 1);
});
