import ChevronLeft from "lucide-react/dist/esm/icons/chevron-left";
import ChevronRight from "lucide-react/dist/esm/icons/chevron-right";

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
	<div className="flex items-center justify-between">
		<div className="flex items-center gap-2">
			<button
				type="button"
				onClick={onPrevious}
				className="rounded-md p-2 hover:bg-slate-100"
			>
				<ChevronLeft className="h-5 w-5" />
			</button>
			<h2 className="text-xl font-semibold min-w-[140px] text-center text-slate-800">
				{year}년 {month}월
			</h2>
			<button
				type="button"
				onClick={onNext}
				className="rounded-md p-2 hover:bg-slate-100"
			>
				<ChevronRight className="h-5 w-5" />
			</button>
		</div>
		<button
			type="button"
			onClick={onToday}
			className="rounded-md border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-100"
		>
			오늘
		</button>
	</div>
);
