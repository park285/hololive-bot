import { adminClient } from "@/app/bootstrap";
import type { CalendarResponse } from "./types";

export const calendarApi = {
	getMonthly: async (
		month: number,
		year: number,
	): Promise<CalendarResponse> =>
		(await adminClient.holoGetCalendar({ month, year })).data,
};
