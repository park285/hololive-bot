import { adminClient } from "@/app/bootstrap";
import type { RequestParams } from "@/api/generated/http-client";
import type { CalendarResponse } from "./types";

/** 월별 조회는 호출자의 취소 신호를 SDK까지 전달합니다. */
export const calendarApi = {
	getMonthly: async (
		month: number,
		year: number,
		{ signal }: RequestParams = {},
	): Promise<CalendarResponse> =>
		(await adminClient.holoGetCalendar({ month, year }, { signal })).data,
};
