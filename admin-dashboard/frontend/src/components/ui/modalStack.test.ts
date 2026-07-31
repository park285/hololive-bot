import assert from "node:assert/strict";
import test from "node:test";
import {
	addModalStackEntry,
	baseModalZIndex,
	type LayeredModalStackEntry,
	removeModalStackEntry,
} from "./modalStack";

const stackEntry = (id: string) => {
	const history: Array<{ zIndex: number; isTop: boolean }> = [];
	const entry: LayeredModalStackEntry & { id: string } = {
		id,
		setStackPresentation: (presentation) => {
			history.push(presentation);
		},
	};
	return { entry, history };
};

test("removing a non-top modal preserves the active top", () => {
	const first = stackEntry("first").entry;
	const middle = stackEntry("middle").entry;
	const top = stackEntry("top").entry;
	const stack = [first, middle, top];

	const result = removeModalStackEntry(stack, middle);

	assert.equal(result.removed, true);
	assert.equal(result.wasTop, false);
	assert.equal(result.nextTop, top);
	assert.deepEqual(stack, [first, top]);
});

test("removing the top modal reveals the previous modal", () => {
	const first = stackEntry("first").entry;
	const top = stackEntry("top").entry;
	const stack = [first, top];

	const result = removeModalStackEntry(stack, top);

	assert.equal(result.removed, true);
	assert.equal(result.wasTop, true);
	assert.equal(result.nextTop, first);
	assert.deepEqual(stack, [first]);
});

test("visual layers and logical top follow the same modal stack order", () => {
	const first = stackEntry("first");
	const second = stackEntry("second");
	const stack: LayeredModalStackEntry[] = [];

	addModalStackEntry(stack, first.entry);
	addModalStackEntry(stack, second.entry);

	assert.deepEqual(first.history.at(-1), {
		zIndex: baseModalZIndex,
		isTop: false,
	});
	assert.deepEqual(second.history.at(-1), {
		zIndex: baseModalZIndex + 1,
		isTop: true,
	});

	removeModalStackEntry(stack, second.entry);
	assert.deepEqual(first.history.at(-1), {
		zIndex: baseModalZIndex,
		isTop: true,
	});
});
