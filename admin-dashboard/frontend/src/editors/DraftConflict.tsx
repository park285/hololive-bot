import { Button } from "@/components/ui/Button";

interface DraftConflictProps { latest: string | undefined; useLatest: () => void; keepDraft: () => void }

/** DraftConflict는 바뀐 저장값과 초안 중 사용자가 선택하도록 하며 동시 갱신 방지를 주장하지 않습니다. */
export function DraftConflict({ latest, useLatest, keepDraft }: DraftConflictProps) {
	return <div role="alert" className="space-y-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-950">
		<p>편집 중 저장된 값이 변경되었습니다: <strong>{latest}</strong></p>
		<p>초안은 보존했습니다. 사용할 값을 선택해 주세요.</p>
		<div className="flex flex-wrap gap-2"><Button type="button" variant="outline" onClick={useLatest}>최신 값 사용</Button><Button type="button" variant="outline" onClick={keepDraft}>이 초안으로 계속</Button></div>
	</div>;
}
