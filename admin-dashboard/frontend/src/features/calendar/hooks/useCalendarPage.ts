import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { queryKeys } from "@/api/queryKeys";
import { calendarApi } from "../api";

export function useCalendarPage() {
	const now = new Date();
	const [month, setMonth] = useState(now.getMonth() + 1);
	const [year, setYear] = useState(now.getFullYear());

	const query = useQuery({
		queryKey: queryKeys.calendar.monthly(month, year),
		queryFn: () => calendarApi.getMonthly(month, year),
	});

	const goToPreviousMonth = () => {
		if (month === 1) {
			setMonth(12);
			setYear((y) => y - 1);
		} else {
			setMonth((m) => m - 1);
		}
	};

	const goToNextMonth = () => {
		if (month === 12) {
			setMonth(1);
			setYear((y) => y + 1);
		} else {
			setMonth((m) => m + 1);
		}
	};

	const goToToday = () => {
		const today = new Date();
		setMonth(today.getMonth() + 1);
		setYear(today.getFullYear());
	};

	return { month, year, query, goToPreviousMonth, goToNextMonth, goToToday };
}
