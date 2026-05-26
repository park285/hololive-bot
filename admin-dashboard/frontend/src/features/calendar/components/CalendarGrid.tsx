import type { CalendarEntry } from "../types";

interface CalendarGridProps {
	entries: CalendarEntry[];
	month: number;
}

function groupByDay(entries: CalendarEntry[]): Map<number, CalendarEntry[]> {
	const grouped = new Map<number, CalendarEntry[]>();
	for (const entry of entries) {
		const existing = grouped.get(entry.day) ?? [];
		existing.push(entry);
		grouped.set(entry.day, existing);
	}
	return grouped;
}

function displayName(entry: CalendarEntry): string {
	const m = entry.member;
	return m.shortKoreanName || m.nameKo || m.name;
}

export const CalendarGrid = ({ entries, month }: CalendarGridProps) => {
	if (entries.length === 0) {
		return (
			<div className="flex items-center justify-center rounded-2xl border border-dashed border-slate-300 p-12">
				<p className="text-slate-500">이 달에 등록된 기념일이 없습니다.</p>
			</div>
		);
	}

	const grouped = groupByDay(entries);

	return (
		<div className="space-y-4">
			{[...grouped.entries()].map(([day, dayEntries]) => (
				<div
					key={day}
					className="rounded-2xl border border-slate-200 p-4"
				>
					<h3 className="mb-3 text-sm font-medium text-slate-500">
						{month}월 {day}일
					</h3>
					<div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
						{dayEntries.map((entry) => (
							<div
								key={`${entry.kind}-${entry.member.channelId}`}
								className="flex items-center gap-3 rounded-xl bg-slate-50 p-3"
							>
								{entry.member.photo && (
									<img
										src={entry.member.photo}
										alt={displayName(entry)}
										className="h-10 w-10 rounded-full object-cover"
									/>
								)}
								<div className="min-w-0 flex-1">
									<p className="truncate text-sm font-medium text-slate-800">
										{displayName(entry)}
									</p>
									<p className="text-xs text-slate-500">
										{entry.kind === "birthday"
											? "🎂 생일"
											: `🎉 데뷔 ${entry.ordinal != null && entry.ordinal > 0 ? `${String(entry.ordinal)}주년` : ""}`}
									</p>
								</div>
							</div>
						))}
					</div>
				</div>
			))}
		</div>
	);
};
