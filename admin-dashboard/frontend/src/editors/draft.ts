/** Draft는 편집 시작 값·마지막 확인 값·사용자 초안을 별도로 보존합니다. */
export interface Draft { base: string | undefined; latest: string | undefined; value: string }

export function initialDraft(value: string | undefined): Draft { return { base: value, latest: value, value: value ?? "" }; }

/** observeDraft는 변경하지 않은 필드만 갱신하며 작성 중인 초안을 외부 값으로 덮어쓰지 않습니다. */
export function observeDraft(draft: Draft, latest: string | undefined): Draft {
	if (latest === undefined || latest === draft.latest) return draft;
	if (draft.latest === undefined || draft.value === draft.latest) return initialDraft(latest);
	return { ...draft, latest };
}

/** draftConflict는 atomic CAS가 아니라 확인한 값이 편집 시작 후 바뀌었다는 표시입니다. */
export function draftConflict(draft: Draft): boolean { return draft.base !== draft.latest && draft.value !== draft.latest; }
