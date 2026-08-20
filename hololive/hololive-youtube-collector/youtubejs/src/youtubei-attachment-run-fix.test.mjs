import assert from "node:assert/strict";
import test from "node:test";

import { applyAttachmentRunLengthFix, upstreamFromAttributedOf } from "./youtubei-attachment-run-fix.mjs";

const attributedTextWithoutLength = () => ({
  content: "Lui ch. and Laplus ch.",
  attachmentRuns: [{ startIndex: 8, element: { type: {}, properties: {} }, alignment: "ALIGNMENT_VERTICAL_CENTER" }],
});

function captureWarnings(t) {
  const warnings = [];
  const originalWarn = console.warn;
  console.warn = (...args) => warnings.push(args);
  t.after(() => {
    console.warn = originalWarn;
  });
  return warnings;
}

test("canary: upstream youtubei.js still drops attachment runs without length", async (t) => {
  const { Misc } = await import("youtubei.js");
  applyAttachmentRunLengthFix(Misc.Text);
  const upstream = upstreamFromAttributedOf(Misc.Text);
  const warnings = captureWarnings(t);

  const parsed = upstream.call(Misc.Text, attributedTextWithoutLength());

  assert.equal(parsed.text, "Lui ch. and Laplus ch.");
  assert.equal(warnings.length, 1, "upstream no longer warns: youtubei.js fixed #1241, remove this shim");
  assert.equal(warnings[0][0], "[YOUTUBEJS][Text]:");
  assert.match(String(warnings[0][1]), /attachment run/);
});

test("shim attaches length-less attachment runs without warning", async (t) => {
  const { Misc } = await import("youtubei.js");
  applyAttachmentRunLengthFix(Misc.Text);
  const warnings = captureWarnings(t);

  const parsed = Misc.Text.fromAttributed(attributedTextWithoutLength());

  assert.deepEqual(warnings, []);
  assert.equal(parsed.text, "Lui ch. and Laplus ch.");
  assert.equal(parsed.runs.length, 1);
  assert.equal(parsed.runs[0].attachment.startIndex, 8);
  assert.equal(parsed.runs[0].attachment.length, 0);
});

test("shim leaves attachment runs that already carry length untouched", async (t) => {
  const { Misc } = await import("youtubei.js");
  applyAttachmentRunLengthFix(Misc.Text);
  const warnings = captureWarnings(t);

  const parsed = Misc.Text.fromAttributed({
    content: "hello :wave:",
    attachmentRuns: [{ startIndex: 6, length: 6, element: { type: { imageType: { image: { sources: [] } } } } }],
  });

  assert.deepEqual(warnings, []);
  assert.equal(parsed.text, "hello :wave:");
  assert.equal(parsed.runs.length, 2);
  assert.equal(parsed.runs[1].text, ":wave:");
});

test("applying the shim twice keeps a single wrapper around the upstream implementation", async () => {
  const { Misc } = await import("youtubei.js");
  applyAttachmentRunLengthFix(Misc.Text);
  const wrapped = Misc.Text.fromAttributed;
  const upstream = upstreamFromAttributedOf(Misc.Text);

  applyAttachmentRunLengthFix(Misc.Text);

  assert.equal(Misc.Text.fromAttributed, wrapped);
  assert.equal(upstreamFromAttributedOf(Misc.Text), upstream);
  assert.notEqual(wrapped, upstream);
});
