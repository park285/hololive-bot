import assert from "node:assert/strict";
import test from "node:test";
import { queryView, type QuerySnapshot } from "@/queries/state";

const snapshot = (changes: Partial<QuerySnapshot<string[]>> = {}): QuerySnapshot<string[]> => ({ status: "success", fetchStatus: "idle", data: ["known"], error: null, isStale: false, dataUpdatedAt: 1234, ...changes });

test("query distinguishes pending, paused, offline, error, empty and successful data", () => {
	for (const [query, online, kind] of [
		[snapshot({ status: "pending", data: undefined, fetchStatus: "fetching" }), true, "pending"],
		[snapshot({ status: "pending", data: undefined, fetchStatus: "paused" }), false, "paused"],
		[snapshot(), false, "offline"],
		[snapshot({ status: "error", data: undefined, error: new Error("failed") }), true, "error"],
		[snapshot({ data: [] }), true, "empty"],
		[snapshot(), true, "success"],
	] as const) assert.equal(queryView(query, online, data => data.length === 0).kind, kind);
});

test("failed refresh preserves previous data and time without making an empty array a successful empty result", () => {
	const view = queryView(snapshot({ status: "error", data: [], error: new Error("failed") }), true, data => data.length === 0);
	assert.equal(view.kind, "error");
	assert.deepEqual(view.data, []);
	assert.equal(view.updatedAt, 1234);
	assert.equal(view.current, false);
});

test("stale or refreshing cached data is not current and an impossible success without data is an error", () => {
	for (const changes of [{ isStale: true }, { fetchStatus: "fetching" as const }]) {
		assert.equal(queryView(snapshot(changes), true, () => false).current, false);
	}
	assert.equal(queryView(snapshot({ data: undefined }), true, () => false).kind, "error");
});
