import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";
import { QueryNotice } from "@/queries/QueryNotice";
import { useCalendarPage } from "../hooks/useCalendarPage";
import { CalendarToolbar } from "../components/CalendarToolbar";
import { CalendarGrid } from "../components/CalendarGrid";

export const CalendarPage = () => {
	const { month, year, query, goToPreviousMonth, goToNextMonth, goToToday } =
		useCalendarPage();
	const view = queryView(query, useOnline(), data => data.entries.length === 0);

	return (
		<div className="space-y-6">
		<div className="flex items-center gap-4 mb-2">
			<div className="w-1 h-12 rounded-full bg-linear-to-b from-rose-400 to-amber-400 shrink-0" />
			<div className="flex flex-col gap-1">
				<h2 className="text-2xl font-display font-bold text-foreground tracking-tight">
					기념일 달력
				</h2>
				<p className="text-muted-foreground">
					홀로멤 생일·데뷔 주년 월별 조회
				</p>
			</div>
		</div>

			<CalendarToolbar
				month={month}
				year={year}
				onPrevious={goToPreviousMonth}
				onNext={goToNextMonth}
				onToday={goToToday}
			/>

			<QueryNotice view={view} label="기념일 달력" onRetry={() => { void query.refetch(); }} />
			{view.data !== undefined && (view.data.entries.length > 0 || view.kind === "empty") && <CalendarGrid entries={view.data.entries} month={month} />}
		</div>
	);
};
