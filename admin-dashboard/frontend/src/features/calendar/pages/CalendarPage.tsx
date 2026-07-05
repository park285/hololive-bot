import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import { useCalendarPage } from "../hooks/useCalendarPage";
import { CalendarToolbar } from "../components/CalendarToolbar";
import { CalendarGrid } from "../components/CalendarGrid";

export const CalendarPage = () => {
	const { month, year, query, goToPreviousMonth, goToNextMonth, goToToday } =
		useCalendarPage();

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

			{query.isLoading ? (
				<div className="flex justify-center items-center h-64 text-subtle-foreground">
					<div className="animate-spin mr-2">
						<Loader2 />
					</div>
					기념일 데이터를 불러오는 중…
				</div>
			) : query.isError ? (
				<div className="rounded-2xl border border-rose-100 bg-rose-50 p-8 text-center text-rose-600">
					<p className="font-bold">
						기념일 데이터를 불러오지 못했습니다.
					</p>
					<p className="mt-2 text-sm">
						{query.error instanceof Error
							? query.error.message
							: "잠시 후 다시 시도해주세요."}
					</p>
					<button
						type="button"
						onClick={() => void query.refetch()}
						className="mt-4 rounded-lg bg-rose-600 px-4 py-2 text-sm font-bold text-white hover:bg-rose-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-300"
					>
						다시 시도
					</button>
				</div>
			) : (
				<CalendarGrid
					entries={query.data?.entries ?? []}
					month={month}
				/>
			)}
		</div>
	);
};
