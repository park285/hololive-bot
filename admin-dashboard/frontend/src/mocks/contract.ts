import { HttpResponse } from "msw";
import { CLIENT_GENERATION } from "@/api/generated/generation";

/** contractJSON은 현재 세대의 정상 fixture header만 추가하며 body는 보정하지 않습니다. */
export const contractJSON: typeof HttpResponse.json = (body, init) => {
	const headers = new Headers(init?.headers);
	headers.set("X-Admin-Server-Generation", CLIENT_GENERATION);
	return HttpResponse.json(body, { ...init, headers });
};
