import type { StreamOrg } from "@/features/streams/types";

export const queryKeys = {
	members: {
		all: ["members"] as const,
		detail: (id: number) => ["members", id] as const,
	},

	alarms: {
		all: ["alarms"] as const,
	},

	rooms: {
		all: ["rooms"] as const,
	},

	stats: {
		summary: ["stats"] as const,
		channels: ["stats", "channels"] as const,
	},

	streams: {
		live: (org: StreamOrg) => ["streams", "live", org] as const,
		upcoming: (org: StreamOrg) => ["streams", "upcoming", org] as const,
	},

	settings: {
		all: ["settings"] as const,
	},

	docker: {
		health: ["docker-health"] as const,
		containers: ["docker-containers"] as const,
	},

	calendar: {
		monthly: (month: number, year: number) =>
			["calendar", month, year] as const,
	},

	milestones: {
		all: ["milestones"] as const,
		near: ["milestones", "near"] as const,
		stats: ["milestones", "stats"] as const,
	},

	status: {
		all: ["status"] as const,
		aggregated: ["status", "aggregated"] as const,
	},
} as const;

export type QueryKeys = typeof queryKeys;
