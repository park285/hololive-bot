import ChevronLeft from "lucide-react/dist/esm/icons/chevron-left.mjs";
import ChevronRight from "lucide-react/dist/esm/icons/chevron-right.mjs";

interface CalendarToolbarProps {
	month: number;
	year: number;
	onPrevious: () => void;
	onNext: () => void;
	onToday: () => void;
}

export const CalendarToolbar = ({
	month,
	year,
	onPrevious,
	onNext,
	onToday,
}: CalendarToolbarProps) => (
	<div className="relative flex flex-wrap items-center justify-between gap-2 bg-card rounded-2xl border border-border-subtle shadow-sm px-5 py-4 overflow-hidden">
		<div className="absolute top-0 left-0 right-0 h-1 bg-linear-to-r from-rose-400 to-amber-400" />
		<div className="flex items-center gap-2">
			<button
				type="button"
				onClick={onPrevious}
				aria-label="이전 월"
				className="rounded-lg p-2 text-muted-foreground hover:bg-accent hover:text-sky-600 transition-colors"
			>
				<ChevronLeft className="h-5 w-5" />
			</button>
			<h2 className="text-xl font-display font-bold min-w-[140px] text-center text-foreground">
				{year}년 {month}월
			</h2>
			<button
				type="button"
				onClick={onNext}
				aria-label="다음 월"
				className="rounded-lg p-2 text-muted-foreground hover:bg-accent hover:text-sky-600 transition-colors"
			>
				<ChevronRight className="h-5 w-5" />
			</button>
		</div>
		<button
			type="button"
			onClick={onToday}
			className="rounded-lg bg-linear-to-r from-sky-500 to-cyan-500 hover:from-sky-600 hover:to-cyan-600 px-4 py-1.5 text-sm font-semibold text-white shadow-sm shadow-sky-200 transition-all"
		>
			오늘
		</button>
	</div>
);
