import ArrowRight from "lucide-react/dist/esm/icons/arrow-right.mjs";
import Bell from "lucide-react/dist/esm/icons/bell.mjs";
import MessageSquare from "lucide-react/dist/esm/icons/message-square.mjs";
import Users from "lucide-react/dist/esm/icons/users.mjs";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface StatsQuickLinksProps {
	onNavigate: (path: string) => void;
}

interface QuickLink {
	label: string;
	path: string;
	Icon: LucideIcon;
	accent: string;
	iconColor: string;
	hoverClasses: string;
	text: string;
}

const links: QuickLink[] = [
	{
		label: "멤버 관리하기",
		path: "/dashboard/members",
		Icon: Users,
		accent: "border-l-sky-400",
		iconColor: "text-sky-500 dark:text-sky-400",
		hoverClasses: "bg-sky-50/60 hover:bg-sky-50 dark:bg-sky-950/40 dark:hover:bg-sky-950/60",
		text: "text-sky-700 dark:text-sky-300",
	},
	{
		label: "알람 설정 확인",
		path: "/dashboard/alarms",
		Icon: Bell,
		accent: "border-l-rose-400",
		iconColor: "text-rose-500 dark:text-rose-400",
		hoverClasses: "bg-rose-50/60 hover:bg-rose-50 dark:bg-rose-950/40 dark:hover:bg-rose-950/60",
		text: "text-rose-700 dark:text-rose-300",
	},
	{
		label: "채팅방 목록",
		path: "/dashboard/rooms",
		Icon: MessageSquare,
		accent: "border-l-indigo-400",
		iconColor: "text-indigo-500 dark:text-indigo-400",
		hoverClasses: "bg-indigo-50/60 hover:bg-indigo-50 dark:bg-indigo-950/40 dark:hover:bg-indigo-950/60",
		text: "text-indigo-700 dark:text-indigo-300",
	},
];

export const StatsQuickLinks = ({ onNavigate }: StatsQuickLinksProps) => (
	<div className="bg-card rounded-2xl border border-border p-6 shadow-sm flex flex-col h-fit animate-fade-in-up stagger-4">
		<h3 className="text-lg font-display font-bold text-foreground mb-4">
			바로가기
		</h3>
		<div className="space-y-3 flex-1">
			{links.map((link) => {
				const { Icon } = link;
				return (
					<button
						key={link.path}
						onClick={() => {
							onNavigate(link.path);
						}}
						className={cn(
							"w-full flex items-center gap-3 p-3.5 rounded-xl border-l-4 transition-all duration-200 group text-left shadow-sm hover:shadow-md hover:-translate-y-0.5",
							link.accent,
							link.hoverClasses,
							link.text,
						)}
					>
						<Icon
							size={20}
							className={cn("shrink-0", link.iconColor)}
							aria-hidden="true"
						/>
						<span className="font-medium flex-1">{link.label}</span>
						<ArrowRight
							size={18}
							className="opacity-40 group-hover:opacity-100 group-hover:translate-x-1 transition-all"
							aria-hidden="true"
						/>
					</button>
				);
			})}
		</div>
	</div>
);
