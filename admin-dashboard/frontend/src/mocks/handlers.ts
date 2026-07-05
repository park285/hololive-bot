import { http, HttpResponse } from "msw";

const nowUnix = () => Math.floor(Date.now() / 1000);

const dockerContainers = [
	{
		id: "container-1",
		name: "hololive-admin-api",
		state: "running",
		status: "Up 2 hours",
		image: "hololive-admin-api:latest",
		health: "healthy",
		managed: true,
		stopBlocked: false,
		created: 1710000000,
		ports: [],
	},
	{
		id: "container-2",
		name: "hololive-bot",
		state: "running",
		status: "Up 1 hour",
		image: "hololive-bot:latest",
		health: "healthy",
		managed: true,
		stopBlocked: false,
		created: 1710000100,
		ports: [],
	},
];

const settings = {
	status: "ok",
	settings: {
		alarmAdvanceMinutes: 5,
	},
};

const rooms = {
	status: "ok",
	rooms: ["200000000000002", "200000000000004"],
	aclEnabled: true,
	aclMode: "blacklist",
};

const members = {
	status: "ok",
	members: [
		{
			id: 1,
			channelId: "UC4",
			name: "Usada Pekora",
			aliases: { ko: ["페코라"], ja: ["ぺこら"] },
			nameJa: "ぺこら",
			nameKo: "페코라",
			isGraduated: false,
		},
		{
			id: 2,
			channelId: "UC1",
			name: "Hoshimachi Suisei",
			aliases: { ko: ["스이세이"], ja: ["すいせい"] },
			nameJa: "すいせい",
			nameKo: "스이세이",
			isGraduated: false,
		},
	],
};

const alarms = {
	status: "ok",
	alarms: [
		{
			roomId: "200000000000002",
			roomName: "운영 방",
			userId: "user-1",
			userName: "관리자",
			channelId: "UC4",
			memberName: "Usada Pekora",
		},
	],
};

const liveStreams = {
	status: "ok",
	org: "hololive",
	streams: [
		{
			id: "stream-1",
			title: "Pekora live now",
			status: "live",
			channel_name: "Usada Pekora",
			channel_id: "UC4",
			link: "https://youtube.com/watch?v=stream-1",
			thumbnail: null,
			start_scheduled: null,
			start_actual: null,
		},
	],
};

const upcomingStreams = {
	status: "ok",
	org: "hololive",
	streams: [
		{
			id: "stream-2",
			title: "Suisei karaoke",
			status: "upcoming",
			channel_name: "Hoshimachi Suisei",
			channel_id: "UC1",
			link: "https://youtube.com/watch?v=stream-2",
			thumbnail: null,
			start_scheduled: new Date(Date.now() + 1000 * 60 * 45).toISOString(),
			start_actual: null,
		},
	],
};

export const handlers = [
	http.get("*/admin/api/auth/session", () =>
		HttpResponse.json({
			status: "ok",
			authenticated: true,
			username: "admin",
			absolute_expires_at: nowUnix() + 60 * 60,
			session_policy: {
				heartbeat_interval_ms: 300000,
				idle_timeout_ms: 600000,
				idle_warning_timeout_ms: 540000,
				idle_session_ttl_ms: 10000,
				absolute_warning_window_ms: 300000,
			},
		}),
	),
	http.post("*/admin/api/auth/login", () =>
		HttpResponse.json({ status: "ok", message: "logged in" }),
	),
	http.post("*/admin/api/auth/logout", () =>
		HttpResponse.json({ status: "ok", message: "logged out" }),
	),
	http.post("*/admin/api/auth/heartbeat", () =>
		HttpResponse.json({
			status: "ok",
			rotated: false,
			absolute_expires_at: nowUnix() + 60 * 60,
		}),
	),
	http.get("*/admin/api/status", () =>
		HttpResponse.json({
			services: [
				{ name: "admin-dashboard", available: true, response_time_ms: 4, error: null },
				{ name: "hololive-bot", available: true, response_time_ms: 12, error: null },
				{ name: "hololive-admin-api", available: true, response_time_ms: 10, error: null },
			],
			uptime: "4h 12m",
			version: "mock-admin-v1",
		}),
	),
	http.get("*/admin/api/holo/stats", () =>
		HttpResponse.json({
			status: "ok",
			members: members.members.length,
			alarms: alarms.alarms.length,
			rooms: rooms.rooms.length,
			version: "mock-bot-v2",
			uptime: "3h 40m",
		}),
	),
	http.get("*/admin/api/docker/health", () =>
		HttpResponse.json({ status: "ok", available: true }),
	),
	http.get("*/admin/api/docker/containers", () =>
		HttpResponse.json({ status: "ok", containers: dockerContainers }),
	),
	http.post("*/admin/api/docker/containers/:name/restart", ({ params }) =>
		HttpResponse.json({
			status: "ok",
			message: `${String(params["name"])} restarted`,
		}),
	),
	http.post("*/admin/api/docker/containers/:name/stop", ({ params }) =>
		HttpResponse.json({
			status: "ok",
			message: `${String(params["name"])} stopped`,
		}),
	),
	http.post("*/admin/api/docker/containers/:name/start", ({ params }) =>
		HttpResponse.json({
			status: "ok",
			message: `${String(params["name"])} started`,
		}),
	),
	http.get("*/admin/api/holo/settings", () => HttpResponse.json(settings)),
	http.post("*/admin/api/holo/settings", async ({ request }) => {
		const nextSettings = (await request.json()) as { alarmAdvanceMinutes?: number };
		return HttpResponse.json({
			status: "ok",
			settings: {
				alarmAdvanceMinutes:
					nextSettings.alarmAdvanceMinutes ?? settings.settings.alarmAdvanceMinutes,
			},
		});
	}),
	http.get("*/admin/api/holo/rooms", () => HttpResponse.json(rooms)),
	http.get("*/admin/api/holo/members", () => HttpResponse.json(members)),
	http.get("*/admin/api/holo/alarms", () => HttpResponse.json(alarms)),
	http.get("*/admin/api/holo/streams/live", () => HttpResponse.json(liveStreams)),
	http.get("*/admin/api/holo/streams/upcoming", () =>
		HttpResponse.json(upcomingStreams),
	),
];
