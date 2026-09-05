import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { createServer } from "vite";

const execFileAsync = promisify(execFile);
const frontendRoot = fileURLToPath(new URL("../../../", import.meta.url));
const actions = ["restart", "stop", "start"];
const outcomes = ["confirmed", "malformed", "server-error", "forbidden"];

async function findBrowser() {
	for (const candidate of [process.env.CHROME_BIN, "/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser"].filter(Boolean)) {
		try {
			await access(candidate);
			return candidate;
		} catch {}
	}
	throw new Error("Chrome or Chromium is required for the Docker action browser contract");
}

const entryModule = String.raw`
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useDockerContainerActions } from "/src/components/settings/DockerContainerActions.tsx";
import apiClient, { setCSRFToken } from "/src/api/client.ts";
import toast, { getToastItems } from "/src/lib/toast-api.ts";

apiClient.defaults.baseURL = window.location.origin + "/__docker_contract_api";
setCSRFToken("browser-contract-csrf");
const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: 2, retryDelay: 0 } } });
const waitFor = async (predicate, label) => {
  for (let attempt = 0; attempt < 200; attempt += 1) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("timed out waiting for " + label);
};
const expect = (condition, message) => { if (!condition) throw new Error(message); };
const App = () => {
  window.actionContract = useDockerContainerActions({
    initialHealth: { status: "ok", available: true },
    initialContainers: { status: "ok", containers: [] },
  });
  return React.createElement("div", null, "Docker action contract");
};
const root = createRoot(document.querySelector("#root"));
root.render(React.createElement(QueryClientProvider, { client }, React.createElement(App)));

const run = async () => {
  await waitFor(() => window.actionContract, "hook render");
  for (const action of ["restart", "stop", "start"]) {
    for (const outcome of ["confirmed", "malformed", "server-error", "forbidden"]) {
      toast.dismiss();
      const name = action + "-" + outcome;
      window.actionContract.openConfirmModal(name, action);
      await waitFor(() => window.actionContract.confirmModal.isOpen && window.actionContract.confirmModal.containerName === name, "confirmation for " + name);
      window.actionContract.handleConfirmAction();
      await waitFor(() => getToastItems().length > 0 && window.actionContract.actionInProgress === null && !window.actionContract.confirmModal.isOpen, "mutation result for " + name);
      const items = getToastItems();
      expect(items.length === 1, name + " emitted multiple notifications");
      if (outcome === "confirmed") {
        expect(items[0].variant === "success", name + " did not report confirmed success");
      } else {
        expect(items[0].variant === "error", name + " incorrectly reported success");
        const message = String(items[0].message);
        if (outcome === "forbidden") {
          expect(message.startsWith("컨테이너 작업 실패:"), name + " lost the explicit refusal");
        } else {
          expect(message.includes("작업 결과 확인 불가:"), name + " asserted failure despite an uncertain outcome");
          expect(message.includes("자동 재시도하지 않았습니다"), name + " omitted retry safety guidance");
        }
      }
    }
  }
};
run().then(
  () => { document.documentElement.dataset.testStatus = "passed"; },
  (error) => {
    document.documentElement.dataset.testStatus = "failed";
    document.querySelector("#result").textContent = error?.stack ?? String(error);
  },
).finally(() => { root.unmount(); client.clear(); toast.dismiss(); });
`;

function fixturePlugin(requestCounts, unexpectedRequests) {
	return {
		name: "docker-action-browser-contract",
		configureServer(server) {
			server.middlewares.use(async (request, response, next) => {
				const pathname = new URL(request.url, "http://localhost").pathname;
				if (pathname === "/__docker_contract_test__") {
					response.setHeader("Content-Type", "text/html");
					response.end(await server.transformIndexHtml(pathname, '<div id="root"></div><pre id="result"></pre><script type="module" src="/__docker_contract_entry.jsx"></script>'));
					return;
				}
				if (!pathname.startsWith("/__docker_contract_api/")) {
					next();
					return;
				}
				response.setHeader("Content-Type", "application/json");
				if (request.method === "GET" && pathname.endsWith("/docker/health")) {
					response.end(JSON.stringify({ status: "ok", available: true }));
					return;
				}
				if (request.method === "GET" && pathname.endsWith("/docker/containers")) {
					response.end(JSON.stringify({ status: "ok", containers: [] }));
					return;
				}
				const match = pathname.match(/^\/__docker_contract_api\/docker\/containers\/([^/]+)\/(restart|stop|start)$/);
				if (request.method !== "POST" || !match || request.headers["x-csrf-token"] !== "browser-contract-csrf") {
					unexpectedRequests.push(`${request.method} ${pathname}`);
					response.statusCode = 400;
					response.end(JSON.stringify({ error: "invalid test request" }));
					return;
				}
				const name = match[1];
				requestCounts.set(name, (requestCounts.get(name) ?? 0) + 1);
				const outcome = name.slice(match[2].length + 1);
				response.statusCode = outcome === "server-error" ? 502 : outcome === "forbidden" ? 403 : 200;
				response.end(JSON.stringify(outcome === "confirmed" ? { status: "ok" } : outcome === "malformed" ? {} : { error: "test refusal" }));
			});
		},
		resolveId(id) {
			if (id === "/__docker_contract_entry.jsx") return "\0virtual:docker-action-browser-contract.jsx";
		},
		load(id) {
			if (id === "\0virtual:docker-action-browser-contract.jsx") return entryModule;
		},
	};
}

test("Docker actions preserve confirmed/unknown/refused outcomes and disable inherited mutation retries", { timeout: 150_000 }, async () => {
	const browser = await findBrowser();
	const userDataDirectory = await mkdtemp(path.join(tmpdir(), "docker-action-browser-"));
	const requestCounts = new Map();
	const unexpectedRequests = [];
	let server;
	try {
		server = await createServer({
			configFile: path.join(frontendRoot, "vite.config.ts"), root: frontendRoot,
			logLevel: "error", plugins: [fixturePlugin(requestCounts, unexpectedRequests)],
			server: { host: "127.0.0.1", port: 0, strictPort: false },
		});
		await server.listen();
		const address = server.httpServer?.address();
		assert(address && typeof address === "object");
		const { stdout, stderr } = await execFileAsync(browser, [
			"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
			`--user-data-dir=${userDataDirectory}`, "--virtual-time-budget=10000", "--dump-dom",
			`http://127.0.0.1:${address.port}/__docker_contract_test__`,
		], { timeout: 120_000, maxBuffer: 4 * 1024 * 1024 });
		assert.match(stdout, /data-test-status="passed"/, `${stderr}\n${stdout}`);
		assert.deepEqual(unexpectedRequests, []);
		assert.equal(requestCounts.size, actions.length * outcomes.length);
		for (const action of actions) {
			for (const outcome of outcomes) {
				assert.equal(requestCounts.get(`${action}-${outcome}`), 1, "mutation must not inherit automatic retry");
			}
		}
	} finally {
		try {
			await server?.close();
		} finally {
			await rm(userDataDirectory, { recursive: true, force: true });
		}
	}
});
