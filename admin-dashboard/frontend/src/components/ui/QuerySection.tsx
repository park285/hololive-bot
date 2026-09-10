import type { ReactNode } from "react";
import { QueryNotice } from "@/queries/QueryNotice";
import type { QueryView } from "@/queries/state";

interface QuerySectionProps<T> {
	view: QueryView<T>;
	label: string;
	onRetry: () => void;
	skeleton?: ReactNode;
	emptyContent: ReactNode;
	children: ReactNode;
}

/** QuerySection은 이전 값을 유지하되 오류·오프라인을 정상 empty로 표시하지 않습니다. */
export function QuerySection<T>({ view, label, onRetry, skeleton, emptyContent, children }: QuerySectionProps<T>) {
	return <div className="space-y-3">
		<QueryNotice view={view} label={label} onRetry={onRetry} />
		{view.kind === "pending" && skeleton}
		{view.kind === "empty" ? emptyContent : view.data !== undefined && children}
	</div>;
}
