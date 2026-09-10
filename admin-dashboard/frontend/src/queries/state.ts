export interface QuerySnapshot<T> {
	status: "pending" | "error" | "success";
	fetchStatus: "fetching" | "paused" | "idle";
	data: T | undefined;
	error: unknown;
	isStale: boolean;
	dataUpdatedAt: number;
}

export type QueryView<T> = {
	kind: "pending" | "paused" | "offline" | "error";
	data: T | undefined;
	current: false;
	updatedAt: number;
	fetching: boolean;
} | {
	kind: "empty" | "success";
	data: T;
	current: boolean;
	updatedAt: number;
	fetching: boolean;
};

/** queryView는 오류·중지·오프라인을 빈 결과로 바꾸지 않고 마지막 성공 값과 현재성을 분리합니다. */
export function queryView<T>(query: QuerySnapshot<T>, online: boolean, empty: (data: T) => boolean): QueryView<T> {
	const previous = { data: query.data, current: false as const, updatedAt: query.dataUpdatedAt, fetching: query.fetchStatus === "fetching" };
	if (query.fetchStatus === "paused") return { ...previous, kind: "paused" };
	if (!online) return { ...previous, kind: "offline" };
	if (query.status === "error") return { ...previous, kind: "error" };
	if (query.status === "pending") return { ...previous, kind: "pending" };
	if (query.data === undefined) return { ...previous, kind: "error" };
	return { kind: empty(query.data) ? "empty" : "success", data: query.data, current: !query.isStale && query.fetchStatus === "idle", updatedAt: query.dataUpdatedAt, fetching: query.fetchStatus === "fetching" };
}
