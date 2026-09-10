import { Button } from "@/components/ui/Button";
import type { QueryView } from "@/queries/state";

interface QueryNoticeProps<T> { view: QueryView<T>; label: string; onRetry: () => void }

/** QueryNotice는 조회 실패/대기/이전 값을 표시하며 업무 변경을 실행하지 않습니다. */
export function QueryNotice<T>({ view, label, onRetry }: QueryNoticeProps<T>) {
	if (view.current) return null;
	const message = view.kind === "paused" ? "연결을 기다리며 조회가 일시 중지되었습니다."
		: view.kind === "offline" ? "오프라인입니다. 최신 상태를 확인할 수 없습니다."
		: view.kind === "error" ? "조회 결과를 확인하지 못했습니다."
		: view.kind === "pending" ? "데이터를 불러오는 중입니다." : "마지막으로 확인한 값입니다.";
	return <div role={view.kind === "error" ? "alert" : "status"} className="rounded-lg border border-border bg-muted p-3 text-sm text-foreground">
		<p className="font-semibold">{label}</p>
		<p>{message}</p>
		{view.data !== undefined && view.kind !== "success" && view.kind !== "empty" && <p>마지막으로 확인한 값을 표시합니다.</p>}
		{(view.kind === "error" || view.kind === "success" || view.kind === "empty") && <Button type="button" variant="outline" className="mt-2" onClick={onRetry} disabled={view.fetching}>다시 조회</Button>}
	</div>;
}
