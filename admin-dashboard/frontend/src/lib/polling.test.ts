import assert from "node:assert/strict";
import test from "node:test";
import { isDocumentVisible, visibleRefetchInterval } from "./polling";

const originalDocument = globalThis.document;

const setVisibility = (state: string | undefined) => {
	if (state === undefined) {
		Reflect.deleteProperty(globalThis, "document");
		return;
	}
	Object.defineProperty(globalThis, "document", {
		value: { visibilityState: state },
		configurable: true,
	});
};

const restoreDocument = () => {
	if (originalDocument === undefined) {
		Reflect.deleteProperty(globalThis, "document");
	} else {
		Object.defineProperty(globalThis, "document", {
			value: originalDocument,
			configurable: true,
		});
	}
};

test("visibleRefetchInterval returns the interval when the document is visible", () => {
	setVisibility("visible");
	assert.equal(isDocumentVisible(), true);
	assert.equal(visibleRefetchInterval(1000)(), 1000);
	restoreDocument();
});

test("visibleRefetchInterval returns false when the document is hidden", () => {
	setVisibility("hidden");
	assert.equal(isDocumentVisible(), false);
	assert.equal(visibleRefetchInterval(1000)(), false);
	restoreDocument();
});

test("isDocumentVisible treats an undefined document as visible", () => {
	setVisibility(undefined);
	assert.equal(isDocumentVisible(), true);
	assert.equal(visibleRefetchInterval(1000)(), 1000);
	restoreDocument();
});
