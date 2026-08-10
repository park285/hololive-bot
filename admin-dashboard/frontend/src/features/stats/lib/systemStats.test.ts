import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const sourcePath = new URL("./systemStats.ts", import.meta.url);
const source = await readFile(sourcePath, "utf8");
const transpiled = ts.transpileModule(
	source.replace(
		'from "@/config"',
		'from "data:text/javascript;base64,ZXhwb3J0IGNvbnN0IENPTkZJRz17dWk6e3NlcnZpY2VDb2xvcnM6e319fTs="',
	),
	{
		compilerOptions: {
			module: ts.ModuleKind.ESNext,
			target: ts.ScriptTarget.ES2022,
		},
	},
);

const mod = await import(
	`data:text/javascript;base64,${Buffer.from(transpiled.outputText).toString("base64")}`
);

test("parseSystemStats supports new runtime contract", () => {
	const parsed = mod.parseSystemStats({
		sampledAt: 1_754_803_200_000,
		cpuUsage: 10,
		memoryUsage: 20,
		memoryTotal: 100,
		memoryUsed: 20,
		threadCount: 7,
		totalGoGoroutines: 42,
		totalRuntimeUnits: 49,
		serviceRuntime: [
			{ name: "admin-dashboard", count: 7, metricKind: "thread", available: true },
			{ name: "hololive-bot", count: 42, metricKind: "goroutine", available: true },
		],
	});

	assert.ok(parsed);
	assert.equal(parsed?.sampledAt, 1_754_803_200_000);
	assert.equal(parsed?.threadCount, 7);
	assert.equal(parsed?.totalGoGoroutines, 42);
	assert.equal(parsed?.serviceRuntime[0]?.metricKind, "thread");
});

test("createSystemStatsPoint preserves backend sample time", () => {
	const stats = mod.parseSystemStats({
		sampledAt: 1_754_803_200_000,
		cpuUsage: 10,
		memoryUsage: 20,
		memoryTotal: 100,
		memoryUsed: 20,
		threadCount: 7,
		totalGoGoroutines: 42,
		totalRuntimeUnits: 42,
		serviceRuntime: [
			{ name: "admin-dashboard", count: 7, available: true },
		],
	});
	assert.ok(stats);
	const point = mod.createSystemStatsPoint(stats, "12:00:00");
	assert.equal(point.timestamp, 1_754_803_200_000);
	assert.equal(point.time, "12:00:00");
	assert.equal(point.serviceValues["admin-dashboard"], 7);
});

test("parseSystemStats still accepts legacy goroutine payload", () => {
	const parsed = mod.parseSystemStats({
		cpuUsage: 10,
		memoryUsage: 20,
		memoryTotal: 100,
		memoryUsed: 20,
		goroutines: 7,
		totalGoroutines: 42,
		serviceGoroutines: [
			{ name: "admin-dashboard", goroutines: 7, available: true },
		],
	});

	assert.ok(parsed);
	assert.equal(parsed?.threadCount, 7);
	assert.equal(parsed?.serviceRuntime[0]?.count, 7);
});

test("shouldConnectSystemStatsStream requires resolved auth state and a visible tab", () => {
	assert.equal(
		mod.shouldConnectSystemStatsStream({
			isAuthenticated: true,
			isAuthResolved: true,
			isVisible: true,
		}),
		true,
	);
	assert.equal(
		mod.shouldConnectSystemStatsStream({
			isAuthenticated: true,
			isAuthResolved: false,
			isVisible: true,
		}),
		false,
	);
	assert.equal(
		mod.shouldConnectSystemStatsStream({
			isAuthenticated: false,
			isAuthResolved: true,
			isVisible: true,
		}),
		false,
	);
	assert.equal(
		mod.shouldConnectSystemStatsStream({
			isAuthenticated: true,
			isAuthResolved: true,
			isVisible: false,
		}),
		false,
	);
});
