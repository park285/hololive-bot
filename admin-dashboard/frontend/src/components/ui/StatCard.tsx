import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

type StatVariant = "blue" | "green" | "yellow" | "rose" | "indigo" | "cyan";

interface StatCardProps {
	label: string;
	value: number | string;
	icon: ReactNode;
	variant?: StatVariant;
	className?: string;
}

const VARIANTS: Record<
	StatVariant,
	{
		bg: string;
		text: string;
		ring: string;
		gradient: string;
		glow: string;
	}
> = {
	blue: {
		bg: "bg-blue-50",
		text: "text-blue-600",
		ring: "ring-blue-100",
		gradient: "from-blue-400 to-blue-500",
		glow: "hover:shadow-blue-200/50",
	},
	green: {
		bg: "bg-emerald-50",
		text: "text-emerald-600",
		ring: "ring-emerald-100",
		gradient: "from-emerald-400 to-emerald-500",
		glow: "hover:shadow-emerald-200/50",
	},
	yellow: {
		bg: "bg-amber-50",
		text: "text-amber-600",
		ring: "ring-amber-100",
		gradient: "from-amber-400 to-amber-500",
		glow: "hover:shadow-amber-200/50",
	},
	rose: {
		bg: "bg-rose-50",
		text: "text-rose-600",
		ring: "ring-rose-100",
		gradient: "from-rose-400 to-rose-500",
		glow: "hover:shadow-rose-200/50",
	},
	indigo: {
		bg: "bg-indigo-50",
		text: "text-indigo-600",
		ring: "ring-indigo-100",
		gradient: "from-indigo-400 to-indigo-500",
		glow: "hover:shadow-indigo-200/50",
	},
	cyan: {
		bg: "bg-cyan-50",
		text: "text-cyan-600",
		ring: "ring-cyan-100",
		gradient: "from-cyan-400 to-cyan-500",
		glow: "hover:shadow-cyan-200/50",
	},
};

export function StatCard({
	label,
	value,
	icon,
	variant = "blue",
	className,
}: StatCardProps) {
	const style = VARIANTS[variant];

	return (
		<div
			className={cn(
				"relative overflow-hidden rounded-2xl border border-slate-100 bg-white shadow-sm transition-all duration-300 hover:-translate-y-1 hover:shadow-lg",
				style.glow,
				className,
			)}
		>
			{/* 상단 그라데이션 액센트 바 */}
			<div
				className={cn(
					"absolute top-0 left-0 right-0 h-1 bg-linear-to-r",
					style.gradient,
				)}
			/>

			<div className="p-6">
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-slate-500 mb-1">{label}</p>
						<h3 className="text-3xl font-display font-bold text-slate-800 tracking-tight tabular-nums">
							{typeof value === "number" ? value.toLocaleString() : value}
						</h3>
					</div>
					{/* 아이콘 - 그라데이션 배경 */}
					<div
						className={cn(
							"relative p-3 rounded-xl ring-4 ring-opacity-50 bg-linear-to-br text-white shadow-sm",
							style.gradient,
							style.ring,
						)}
					>
						{icon}
					</div>
				</div>
			</div>

			{/* 장식용 원형 - 우하단 */}
			<div
				className={cn(
					"absolute -bottom-6 -right-6 w-24 h-24 rounded-full opacity-10 bg-linear-to-br",
					style.gradient,
				)}
			/>
		</div>
	);
}
