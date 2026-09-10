const focusableSelector = 'a[href],button,input,select,textarea,[contenteditable="true"],[tabindex]';

/** dialogFocusTargets는 숨김·inert·비활성 요소를 제외한 실제 Tab 순서를 반환합니다. */
export function dialogFocusTargets(dialog: HTMLElement): HTMLElement[] {
	return Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector)).filter(element => element.tabIndex >= 0 && !element.matches(":disabled") && !element.closest("[inert]") && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== "hidden");
}

/** trapDialogTab은 모달 또는 모바일 drawer의 Tab/Shift+Tab을 내부에서 순환시킵니다. */
export function trapDialogTab(event: Pick<KeyboardEvent, "key" | "shiftKey" | "defaultPrevented" | "preventDefault">, dialog: HTMLElement): void {
	if (event.key !== "Tab" || event.defaultPrevented) return;
	const targets = dialogFocusTargets(dialog);
	const [first] = targets;
	const last = targets.at(-1);
	if (!first || !last) { event.preventDefault(); dialog.focus(); return; }
	const active = dialog.ownerDocument.activeElement;
	if (event.shiftKey && (active === first || active === dialog)) { event.preventDefault(); last.focus(); }
	else if (!event.shiftKey && active === last) { event.preventDefault(); first.focus(); }
}
