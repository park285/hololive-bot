import { type ComponentType, type LazyExoticComponent, lazy } from "react";

export interface RouteDefinition {
	id: string;
	path: string;
	absolutePath: string;
	load: () => Promise<{ default: ComponentType }>;
}

export const ROUTE_DEFINITIONS: RouteDefinition[] = [
	{
		id: "stats",
		path: "stats",
		absolutePath: "/dashboard/stats",
		load: () =>
			import("@/features/stats/pages/StatsPage").then((module) => ({
				default: module.StatsPage,
			})),
	},
	{
		id: "streams",
		path: "streams",
		absolutePath: "/dashboard/streams",
		load: () =>
			import("@/features/streams/pages/StreamsPage").then((module) => ({
				default: module.StreamsPage,
			})),
	},
	{
		id: "members",
		path: "members",
		absolutePath: "/dashboard/members",
		load: () =>
			import("@/features/members/pages/MembersPage").then((module) => ({
				default: module.MembersPage,
			})),
	},
	{
		id: "milestones",
		path: "milestones",
		absolutePath: "/dashboard/milestones",
		load: () =>
			import("@/features/milestones/pages/MilestonesPage").then((module) => ({
				default: module.MilestonesPage,
			})),
	},
	{
		id: "calendar",
		path: "calendar",
		absolutePath: "/dashboard/calendar",
		load: () =>
			import("@/features/calendar/pages/CalendarPage").then((module) => ({
				default: module.CalendarPage,
			})),
	},
	{
		id: "alarms",
		path: "alarms",
		absolutePath: "/dashboard/alarms",
		load: () =>
			import("@/features/alarms/pages/AlarmsPage").then((module) => ({
				default: module.AlarmsPage,
			})),
	},
	{
		id: "rooms",
		path: "rooms",
		absolutePath: "/dashboard/rooms",
		load: () =>
			import("@/features/rooms/pages/RoomsPage").then((module) => ({
				default: module.RoomsPage,
			})),
	},
	{
		id: "settings",
		path: "settings",
		absolutePath: "/dashboard/settings",
		load: () =>
			import("@/features/settings/pages/SettingsPage").then((module) => ({
				default: module.SettingsPage,
			})),
	},
];

const routeDefinitionsById = new Map(
	ROUTE_DEFINITIONS.map((route) => [route.id, route] as const),
);
const lazyCache = new Map<string, LazyExoticComponent<ComponentType>>();

export const getLazyComponent = (id: string) => {
	const cachedComponent = lazyCache.get(id);
	if (cachedComponent) {
		return cachedComponent;
	}

	const route = routeDefinitionsById.get(id);
	if (!route) {
		throw new Error(`Route ${id} not found in route definitions`);
	}

	const lazyComponent = lazy(route.load);
	lazyCache.set(id, lazyComponent);
	return lazyComponent;
};

const prefetchedSet = new Set<string>();

export const prefetchRoute = (id: string) => {
	if (prefetchedSet.has(id)) return;

	const route = routeDefinitionsById.get(id);
	if (!route) return;

	prefetchedSet.add(id);
	void route.load();
};
