import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const execFileAsync = promisify(execFile);
const frontendRoot = fileURLToPath(new URL("../../../", import.meta.url));

const browserCandidates = [
	process.env.CHROME_BIN,
	"/usr/bin/google-chrome",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/chromium",
	"/usr/bin/chromium-browser",
].filter(Boolean);

const findBrowser = async () => {
	for (const candidate of browserCandidates) {
		try {
			await access(candidate);
			return candidate;
		} catch {}
	}
	return null;
};

const entryModule = String.raw`
import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { BaseModal } from "/src/components/ui/BaseModal.tsx";

const nextFrame = () => new Promise((resolve) => setTimeout(resolve, 10));
const waitFor = async (predicate, label) => {
  for (let attempt = 0; attempt < 60; attempt += 1) {
    if (predicate()) return;
    await nextFrame();
  }
  throw new Error("timed out waiting for " + label);
};
const press = (target, key, shiftKey = false) => {
  target.dispatchEvent(new KeyboardEvent("keydown", { key, shiftKey, bubbles: true, cancelable: true }));
};
const expect = (condition, message) => {
  if (!condition) throw new Error(message);
};

const App = () => {
  const [baseOpen, setBaseOpen] = useState(false);
  const [middleOpen, setMiddleOpen] = useState(false);
  const [topOpen, setTopOpen] = useState(false);
  const [baseCloseCount, setBaseCloseCount] = useState(0);
  const [topCloseCount, setTopCloseCount] = useState(0);

  window.modalTestControls = {
    setBaseOpen,
    setMiddleOpen,
    setTopOpen,
    getBaseCloseCount: () => baseCloseCount,
    getTopCloseCount: () => topCloseCount,
  };

  return React.createElement(
    React.Fragment,
    null,
    React.createElement("button", { id: "opener", type: "button", onClick: () => setBaseOpen(true) }, "Open"),
    React.createElement(
      BaseModal,
      {
        isOpen: baseOpen,
        title: "Base",
        onClose: () => {
          setBaseCloseCount((count) => count + 1);
          setBaseOpen(false);
        },
      },
      React.createElement("button", { id: "base-first", type: "button" }, "First"),
      React.createElement("button", { id: "base-last", type: "button" }, "Last"),
    ),
    React.createElement(
      BaseModal,
      { isOpen: middleOpen, title: "Middle", onClose: () => setMiddleOpen(false) },
      React.createElement("button", { id: "middle-first", type: "button" }, "Middle first"),
    ),
    React.createElement(
      BaseModal,
      {
        isOpen: topOpen,
        title: "Top",
        onClose: () => {
          setTopCloseCount((count) => count + 1);
          setTopOpen(false);
        },
      },
      React.createElement("button", { id: "top-first", type: "button" }, "Top first"),
    ),
  );
};

const run = async () => {
  await waitFor(() => document.querySelector("#opener"), "application render");
  const opener = document.querySelector("#opener");
  opener.focus();
  opener.click();
  await waitFor(() => document.activeElement?.id === "base-first", "initial modal focus");
  const root = document.querySelector("#root");
  const preInert = document.querySelector("#pre-inert");
  const portalHost = document.querySelector("[data-base-modal-portal]");
  expect(root.inert === true, "application root is not inert for a single modal");
  expect(preInert.inert === true, "preexisting inert sibling lost its state while open");
  expect(portalHost?.parentElement === document.body, "modal portal is not a body child");
  expect(portalHost.contains(document.querySelector("[role='dialog']")), "dialog is not rendered in the portal");

  const baseFirst = document.querySelector("#base-first");
  const baseLast = document.querySelector("#base-last");

  opener.focus();
  expect(document.activeElement === baseFirst, "programmatic focus escaped to the inert application");

  press(baseFirst, "Tab", true);
  expect(document.activeElement === baseLast, "Shift+Tab did not wrap to the final control");
  press(baseLast, "Tab");
  expect(document.activeElement === baseFirst, "Tab did not wrap to the first control");

  window.modalTestControls.setMiddleOpen(true);
  await waitFor(() => document.activeElement?.id === "middle-first", "middle modal focus");
  window.modalTestControls.setTopOpen(true);
  await waitFor(() => document.activeElement?.id === "top-first", "top modal focus");

  const dialogs = [...document.querySelectorAll("[role='dialog']")];
  expect(dialogs.length === 3, "expected three rendered dialogs");
  for (const dialog of dialogs.slice(0, -1)) {
    expect(dialog.parentElement.parentElement.inert === true, "non-top modal is not inert");
    expect(dialog.getAttribute("aria-modal") === null, "non-top modal kept aria-modal");
  }
  expect(dialogs.at(-1).getAttribute("aria-modal") === "true", "top modal lacks aria-modal");
  expect(dialogs.at(-1).parentElement.parentElement.inert === false, "top modal is inert");
  expect(root.inert === true, "nested modal stack re-enabled the application root");

  window.modalTestControls.setMiddleOpen(false);
  await waitFor(() => document.querySelectorAll("[role='dialog']").length === 2, "non-top modal removal");
  expect(document.activeElement?.id === "top-first", "non-top removal stole focus from the top modal");
  expect(root.inert === true, "non-top removal re-enabled the application root");

  const [baseDialog, topDialog] = document.querySelectorAll("[role='dialog']");
  press(baseDialog, "Escape");
  await nextFrame();
  expect(window.modalTestControls.getBaseCloseCount() === 0, "non-top Escape closed the base modal");
  expect(document.activeElement?.id === "top-first", "non-top Escape did not preserve top focus");

  press(topDialog, "Escape");
  await waitFor(() => document.querySelectorAll("[role='dialog']").length === 1, "top Escape close");
  expect(window.modalTestControls.getTopCloseCount() === 1, "top Escape did not close exactly once");
  expect(document.activeElement?.id === "base-first", "closing the top modal did not restore nested focus");

  press(document.querySelector("[role='dialog']"), "Escape");
  await waitFor(() => document.querySelectorAll("[role='dialog']").length === 0, "base Escape close");
  expect(window.modalTestControls.getBaseCloseCount() === 1, "base Escape did not close exactly once");
  expect(document.activeElement === opener, "closing the final modal did not restore opener focus");
  expect(document.body.style.overflow === "", "body overflow was not restored");
  expect(root.inert === false, "application root inert state was not restored");
  expect(preInert.inert === true, "preexisting inert sibling state was not restored");
};

createRoot(document.querySelector("#root")).render(React.createElement(App));
run().then(
  () => {
    document.documentElement.dataset.testStatus = "passed";
  },
  (error) => {
    document.documentElement.dataset.testStatus = "failed";
    document.querySelector("#result").textContent = error?.stack ?? String(error);
  },
);
`;

const browserFixturePlugin = {
	name: "base-modal-browser-fixture",
	configureServer(server) {
		server.middlewares.use(async (request, response, next) => {
			if (request.url !== "/__base-modal_test__") {
				next();
				return;
			}
			response.statusCode = 200;
			response.setHeader("Content-Type", "text/html");
			response.end(
				await server.transformIndexHtml(
					request.url,
					'<div id="root"></div><aside id="pre-inert" inert></aside><pre id="result"></pre><script type="module" src="/__base-modal_test_entry.jsx"></script>',
				),
			);
		});
	},
	resolveId(id) {
		if (id === "/__base-modal_test_entry.jsx") {
			return "\0virtual:base-modal-browser-test.jsx";
		}
	},
	load(id) {
		if (id === "\0virtual:base-modal-browser-test.jsx") {
			return entryModule;
		}
	},
};

test("BaseModal enforces browser focus, keyboard, nesting, and ARIA behavior", async (context) => {
	const browser = await findBrowser();
	if (!browser) {
		context.skip("Chrome or Chromium is required for the BaseModal browser contract");
		return;
	}

	const userDataDirectory = await mkdtemp(
		path.join(tmpdir(), "base-modal-browser-"),
	);
	const server = await createServer({
		configFile: path.join(frontendRoot, "vite.config.ts"),
		root: frontendRoot,
		logLevel: "error",
		plugins: [browserFixturePlugin],
		server: { host: "127.0.0.1", port: 0, strictPort: false },
	});

	try {
		await server.listen();
		const address = server.httpServer?.address();
		assert(address && typeof address === "object");
		const { stdout, stderr } = await execFileAsync(
			browser,
			[
				"--headless=new",
				"--no-sandbox",
				"--disable-gpu",
				"--disable-dev-shm-usage",
				`--user-data-dir=${userDataDirectory}`,
				"--virtual-time-budget=5000",
				"--dump-dom",
				`http://127.0.0.1:${address.port}/__base-modal_test__`,
			],
			{ maxBuffer: 4 * 1024 * 1024, timeout: 30_000 },
		);
		assert.match(stdout, /data-test-status="passed"/, `${stderr}\n${stdout}`);
	} finally {
		await server.close();
		await rm(userDataDirectory, { recursive: true, force: true });
	}
});
