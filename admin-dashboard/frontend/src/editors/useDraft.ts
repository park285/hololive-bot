import { useEffect, useState } from "react";
import { draftConflict, initialDraft, observeDraft } from "@/editors/draft";

/** useDraft는 조회/저장 실패에도 초안을 유지하며 외부 변경의 수용을 명시적으로 처리합니다. */
export function useDraft(latest: string | undefined) {
	const [draft, setDraft] = useState(() => initialDraft(latest));
	useEffect(() => { setDraft(previous => observeDraft(previous, latest)); }, [latest]);
	return {
		...draft,
		dirty: draft.latest !== undefined && draft.value !== draft.latest,
		conflict: draftConflict(draft),
		setValue: (value: string) => { setDraft(previous => ({ ...previous, value })); },
		useLatest: () => { setDraft(previous => initialDraft(previous.latest)); },
		keepDraft: () => { setDraft(previous => ({ ...previous, base: previous.latest })); },
		committed: (value: string) => { setDraft(initialDraft(value)); },
	};
}
