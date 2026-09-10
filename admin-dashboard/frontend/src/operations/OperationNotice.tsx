import { useSyncExternalStore } from "react";
import { operations } from "@/app/bootstrap";
import { Button } from "@/components/ui/Button";

const effectLabels = { confirmed: "확인됨", failed: "실패", unknown: "확인 필요", not_needed: "변경할 항목 없음" };

/** OperationNotice는 화면 이동 후에도 같은 인증의 마지막 작업 결과를 표시합니다. */
export function OperationNotice() {
	const snapshot = useSyncExternalStore(operations.subscribe, operations.snapshot);
	const {result} = snapshot;
	if (!snapshot.busy && !result) return null;
	return <aside className="border-b border-border bg-card px-4 py-3 text-sm text-foreground" aria-label="업무 변경 결과">
		{snapshot.busy ? <p role="status">{snapshot.activeLabel ? `${snapshot.activeLabel} 처리 중입니다.` : "요청 응답을 기다리고 있습니다."}</p> : result && <div className="flex items-start justify-between gap-3">
			<div role={result.outcome.kind === "unknown" || result.outcome.kind === "failed" ? "alert" : "status"}>
				<p className="font-semibold">{result.label}</p>
				<p>{result.outcome.message}</p>
				{result.outcome.effects.length > 1 && <ul className="mt-1 flex flex-wrap gap-x-4">{result.outcome.effects.map(effect => <li key={effect.key}>{effect.label}: {effectLabels[effect.state]}</li>)}</ul>}
			</div>
			<Button type="button" variant="ghost" onClick={operations.dismiss} aria-label="작업 결과 닫기">닫기</Button>
		</div>}
	</aside>;
}
