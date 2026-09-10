import assert from "node:assert/strict";
import test from "node:test";
import { draftConflict, initialDraft, observeDraft } from "@/editors/draft";

test("unknown reads do not invent a value and server range is preserved without editor clamping", () => {
	assert.deepEqual(initialDraft(undefined), { base: undefined, latest: undefined, value: "" });
	for (const value of ["0", "15", "120", "1440"]) assert.equal(observeDraft(initialDraft(undefined), value).value, value);
});

test("an external update preserves the edit base and draft and requires a choice", () => {
	const edited = { ...initialDraft("15"), value: "20" };
	const changed = observeDraft(edited, "25");
	assert.deepEqual(changed, { base: "15", latest: "25", value: "20" });
	assert.equal(draftConflict(changed), true);
	assert.equal(draftConflict({ ...changed, base: changed.latest }), false);
	assert.equal(observeDraft(changed, undefined), changed, "failed/unknown retrieval cannot erase the draft");
});

test("an unchanged field follows a successful fresh read", () => {
	assert.deepEqual(observeDraft(initialDraft("15"), "25"), initialDraft("25"));
});
