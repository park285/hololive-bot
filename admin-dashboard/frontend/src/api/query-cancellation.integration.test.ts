import assert from "node:assert/strict";
import { after, before, test } from "node:test";
import { createServer } from "node:http";
import { QueryClient, isCancelledError } from "@tanstack/react-query";
import { generation, httpClient } from "@/app/bootstrap";
import { membersApi } from "@/features/members/api";
import { roomsApi } from "@/features/rooms/api";
import { alarmsApi } from "@/features/alarms/api";
import { statsApi } from "@/features/stats/api";
import { settingsApi } from "@/features/settings/api";
import { calendarApi } from "@/features/calendar/api";
import { streamsApi } from "@/features/streams/api";
import { dockerApi } from "@/features/docker/api";

const previousBaseURL = httpClient.defaults.baseURL;
before(() => {
	generation.ready();
});
after(() => { httpClient.defaults.baseURL = previousBaseURL; });

const reads: Array<[string, (signal: AbortSignal) => Promise<unknown>]> = [
	["members", signal => membersApi.getAll({ signal })],
	["rooms", signal => roomsApi.getAll({ signal })],
	["joined rooms", signal => roomsApi.getJoined({ signal })],
	["alarms", signal => alarmsApi.getAll({ signal })],
	["status", signal => statsApi.getStatus({ signal })],
	["stats", signal => statsApi.get({ signal })],
	["settings", signal => settingsApi.get({ signal })],
	["calendar", signal => calendarApi.getMonthly(9, 2026, { signal })],
	["live", signal => streamsApi.getLive("hololive", { signal })],
	["upcoming", signal => streamsApi.getUpcoming("hololive", { signal })],
	["docker health", signal => dockerApi.checkHealth({ signal })],
	["containers", signal => dockerApi.getContainers({ signal })],
];

for (const [name, read] of reads) {
	test(`${name}: cancelling the query aborts the pending HTTP request`, { timeout: 5000 }, async () => {
		const received = Promise.withResolvers<void>();
		const aborted = Promise.withResolvers<void>();
		const server = createServer((_request, response) => {
			response.once("close", () => aborted.resolve());
			received.resolve();
		});
		await new Promise<void>(resolve => { server.listen(0, "127.0.0.1", resolve); });
		const address = server.address();
		assert.ok(address && typeof address !== "string");
		httpClient.defaults.baseURL = `http://127.0.0.1:${address.port}`;
		const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		try {
			const result = client.fetchQuery({ queryKey: [name], queryFn: ({ signal }) => read(signal) }).catch((error: unknown) => error);
			await received.promise;
			await client.cancelQueries();
			await aborted.promise;
			assert.ok(isCancelledError(await result));
			assert.equal(client.getQueryData([name]), undefined);
		} finally {
			client.clear();
			server.closeAllConnections();
			await new Promise<void>((resolve, reject) => { server.close(error => { if (error) reject(error); else resolve(); }); });
		}
	});
}
