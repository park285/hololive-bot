const serviceColors: Record<string, string> = {
	"hololive-bot": "#0ea5e9",
	"admin-dashboard": "#64748b",
};

export const CONFIG = {
	heartbeat: {
		intervalMs: 5 * 60 * 1000,
		idleTimeoutMs: 10 * 60 * 1000,
		maxFailures: 3,
	},

	websocket: {
		reconnectAttempts: 5,
		reconnectIntervalMs: 3000,
		maxBackoffMs: 30000,
		pingIntervalMs: 30 * 1000,
	},

	query: {
		staleTimeMs: 5 * 60 * 1000,
		gcTimeMs: 60 * 60 * 1000,
		retry: 1,
	},

	api: {
		timeoutMs: 30000,
		baseUrl: "/admin/api",
	},

	ui: {
		serviceColors,
	},
} as const;

export type AppConfig = typeof CONFIG;
