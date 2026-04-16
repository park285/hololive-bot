import type { LucideIcon } from "lucide-react";
import Bell from "lucide-react/dist/esm/icons/bell";
import LayoutDashboard from "lucide-react/dist/esm/icons/layout-dashboard";
import MessageSquare from "lucide-react/dist/esm/icons/message-square";
import Radio from "lucide-react/dist/esm/icons/radio";
import Settings from "lucide-react/dist/esm/icons/settings";
import Trophy from "lucide-react/dist/esm/icons/trophy";
import Users from "lucide-react/dist/esm/icons/users";
import { prefetchRoute, ROUTE_DEFINITIONS } from "@/routes/route-definitions";

export type RouteGroup = "개요" | "홀로라이브 봇" | "인프라";

export interface RouteManifestItem {
	id: string;
	path: string;
	absolutePath: string;
	label: string;
	icon: LucideIcon;
	group: RouteGroup;
}

const ROUTE_METADATA: Record<
	string,
	Omit<RouteManifestItem, "path" | "absolutePath">
> = {
	stats: {
		id: "stats",
		label: "통합 대시보드",
		icon: LayoutDashboard,
		group: "개요",
	},
	streams: {
		id: "streams",
		label: "방송 현황",
		icon: Radio,
		group: "홀로라이브 봇",
	},
	members: {
		id: "members",
		label: "멤버 관리",
		icon: Users,
		group: "홀로라이브 봇",
	},
	milestones: {
		id: "milestones",
		label: "마일스톤",
		icon: Trophy,
		group: "홀로라이브 봇",
	},
	alarms: {
		id: "alarms",
		label: "알람 관리",
		icon: Bell,
		group: "홀로라이브 봇",
	},
	rooms: {
		id: "rooms",
		label: "방 관리",
		icon: MessageSquare,
		group: "홀로라이브 봇",
	},
	settings: {
		id: "settings",
		label: "설정",
		icon: Settings,
		group: "인프라",
	},
};

export const ROUTE_MANIFEST: RouteManifestItem[] = ROUTE_DEFINITIONS.map(
	(route) => {
		const metadata = ROUTE_METADATA[route.id];
		if (!metadata) {
			throw new Error(`Route ${route.id} metadata not found`);
		}

		return {
			...metadata,
			path: route.path,
			absolutePath: route.absolutePath,
		};
	},
);

export { prefetchRoute };

const NAV_GROUP_ORDER: RouteGroup[] = ["개요", "홀로라이브 봇", "인프라"];

export const NAV_GROUPS = NAV_GROUP_ORDER.flatMap((groupName) => {
	const items = ROUTE_MANIFEST.filter((route) => route.group === groupName);
	return items.length > 0 ? [{ title: groupName, items }] : [];
});

export const getNavGroups = () => NAV_GROUPS;
