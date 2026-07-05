export const isDocumentVisible = (): boolean =>
	typeof document === "undefined" || document.visibilityState === "visible";

export const visibleRefetchInterval =
	(intervalMs: number) =>
	(): number | false =>
		isDocumentVisible() ? intervalMs : false;
