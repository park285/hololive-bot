import assert from "node:assert/strict";
import test from "node:test";
import { sectionStateProps } from "./queryState";

test("sectionStateProps forwards the query flags and error verbatim", () => {
	const boom = new Error("boom");
	const props = sectionStateProps({
		isLoading: true,
		isError: false,
		error: boom,
		refetch: () => undefined,
	});

	assert.equal(props.isLoading, true);
	assert.equal(props.isError, false);
	assert.equal(props.error, boom);
});

test("onRetry invokes refetch exactly once and swallows its promise", () => {
	let calls = 0;
	const props = sectionStateProps({
		isLoading: false,
		isError: true,
		error: null,
		refetch: () => {
			calls += 1;
			return Promise.resolve();
		},
	});

	const result = props.onRetry();

	assert.equal(calls, 1);
	assert.equal(result, undefined);
});
