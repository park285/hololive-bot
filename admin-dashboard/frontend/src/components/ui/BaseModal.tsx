import clsx from "clsx";
import {
	type KeyboardEvent as ReactKeyboardEvent,
	type ReactNode,
	useId,
	useLayoutEffect,
	useRef,
	useState,
} from "react";
import { createPortal } from "react-dom";
import {
	addModalStackEntry,
	baseModalZIndex,
	type LayeredModalStackEntry,
	removeModalStackEntry,
} from "./modalStack";

interface BaseModalProps {
	isOpen: boolean;
	onClose: () => void;
	title: ReactNode;
	children: ReactNode;
	maxWidth?: "sm" | "md" | "lg" | "xl" | "2xl";
	titleClassName?: string;
	showHeaderBorder?: boolean;
}

const maxWidthClasses = {
	sm: "max-w-sm",
	md: "max-w-md",
	lg: "max-w-lg",
	xl: "max-w-xl",
	"2xl": "max-w-2xl",
};

const focusableSelector = [
	'a[href]',
	'button:not([disabled])',
	'input:not([disabled])',
	'select:not([disabled])',
	'textarea:not([disabled])',
	'[contenteditable="true"]',
	'[tabindex]:not([tabindex="-1"])',
].join(",");

interface ModalStackEntry extends LayeredModalStackEntry {
	dialog: HTMLDivElement;
	lastFocusedInside: HTMLElement | null;
}

const openModalStack: ModalStackEntry[] = [];
let originalBodyOverflow: string | undefined;
let externalFocusTarget: HTMLElement | null = null;
let modalPortalHost: HTMLDivElement | null = null;
let backgroundInertSnapshots:
	| Array<{ element: HTMLElement; inert: boolean }>
	| undefined;

const ensureModalPortalHost = () => {
	if (modalPortalHost?.isConnected) return modalPortalHost;

	const existingHost = document.querySelector<HTMLDivElement>(
		"[data-base-modal-portal]",
	);
	modalPortalHost = existingHost ?? document.createElement("div");
	if (!existingHost) {
		modalPortalHost.dataset["baseModalPortal"] = "";
		document.body.append(modalPortalHost);
	}
	return modalPortalHost;
};

const makeBackgroundInert = (portalHost: HTMLElement) => {
	backgroundInertSnapshots = Array.from(document.body.children)
		.filter(
			(element): element is HTMLElement =>
				element instanceof HTMLElement && element !== portalHost,
		)
		.map((element) => ({ element, inert: element.inert }));
	for (const snapshot of backgroundInertSnapshots) {
		snapshot.element.inert = true;
	}
};

const restoreBackgroundInert = () => {
	for (const snapshot of backgroundInertSnapshots ?? []) {
		if (snapshot.element.isConnected) {
			snapshot.element.inert = snapshot.inert;
		}
	}
	backgroundInertSnapshots = undefined;
};

const isValidFocusTarget = (
	entry: ModalStackEntry,
	target: HTMLElement | null,
) =>
	target?.isConnected === true &&
	entry.dialog.contains(target) &&
	!target.matches(":disabled, [hidden], [aria-hidden='true']");

const focusModalEntry = (entry: ModalStackEntry) => {
	const rememberedTarget = isValidFocusTarget(entry, entry.lastFocusedInside)
		? entry.lastFocusedInside
		: null;
	const firstFocusableElement =
		entry.dialog.querySelector<HTMLElement>(focusableSelector);
	const focusTarget = rememberedTarget ?? firstFocusableElement ?? entry.dialog;

	focusTarget.focus({ preventScroll: true });
	entry.lastFocusedInside = focusTarget;
};

export const BaseModal = ({
	isOpen,
	onClose,
	title,
	children,
	maxWidth = "md",
	titleClassName,
	showHeaderBorder = false,
}: BaseModalProps) => {
	const dialogRef = useRef<HTMLDivElement>(null);
	const stackEntryRef = useRef<ModalStackEntry | null>(null);
	const [portalHost, setPortalHost] = useState<HTMLDivElement | null>(null);
	const [stackPresentation, setStackPresentation] = useState({
		zIndex: baseModalZIndex,
		isTop: true,
	});
	const titleId = useId();

	useLayoutEffect(() => {
		if (isOpen) {
			setPortalHost(ensureModalPortalHost());
		}
	}, [isOpen]);

	useLayoutEffect(() => {
		if (!isOpen || !portalHost) return;

		const dialog = dialogRef.current;
		if (!dialog) return;

		const currentTop = openModalStack.at(-1);
		const { activeElement } = document;
		if (
			currentTop &&
			activeElement instanceof HTMLElement &&
			currentTop.dialog.contains(activeElement)
		) {
			currentTop.lastFocusedInside = activeElement;
		}

		if (openModalStack.length === 0) {
			externalFocusTarget =
				activeElement instanceof HTMLElement ? activeElement : null;
			originalBodyOverflow = document.body.style.overflow;
			document.body.style.overflow = "hidden";
			makeBackgroundInert(portalHost);
		}

		const entry: ModalStackEntry = {
			dialog,
			lastFocusedInside: null,
			setStackPresentation,
		};
		stackEntryRef.current = entry;
		addModalStackEntry(openModalStack, entry);
		focusModalEntry(entry);

		return () => {
			const result = removeModalStackEntry(openModalStack, entry);
			stackEntryRef.current = null;
			if (!result.removed) return;

			if (openModalStack.length === 0) {
				restoreBackgroundInert();
				document.body.style.overflow = originalBodyOverflow ?? "";
				originalBodyOverflow = undefined;

				if (externalFocusTarget?.isConnected) {
					externalFocusTarget.focus({ preventScroll: true });
				}
				externalFocusTarget = null;
			} else if (result.wasTop && result.nextTop) {
				const { nextTop } = result;
				queueMicrotask(() => {
					if (openModalStack.at(-1) === nextTop) {
						focusModalEntry(nextTop);
					}
				});
			}
		};
	}, [isOpen, portalHost]);

	const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
		const entry = stackEntryRef.current;
		const topEntry = openModalStack.at(-1);
		if (entry && topEntry && entry !== topEntry) {
			event.preventDefault();
			event.stopPropagation();
			focusModalEntry(topEntry);
			return;
		}

		if (event.key === "Escape") {
			event.preventDefault();
			event.stopPropagation();
			onClose();
			return;
		}

		if (event.key !== "Tab") return;

		const dialog = dialogRef.current;
		if (!dialog) return;

		const focusableElements = Array.from(
			dialog.querySelectorAll<HTMLElement>(focusableSelector),
		);
		if (focusableElements.length === 0) {
			event.preventDefault();
			dialog.focus();
			return;
		}

		const [firstFocusableElement] = focusableElements;
		const lastFocusableElement = focusableElements.at(-1);
		const { activeElement } = document;

		if (
			event.shiftKey &&
			(activeElement === firstFocusableElement || activeElement === dialog)
		) {
			event.preventDefault();
			lastFocusableElement?.focus();
		} else if (!event.shiftKey && activeElement === lastFocusableElement) {
			event.preventDefault();
			firstFocusableElement?.focus();
		}
	};

	if (!isOpen || !portalHost) {
		return null;
	}

	return createPortal(
		<div
			className="fixed inset-0 z-50"
			style={{ zIndex: stackPresentation.zIndex }}
			aria-hidden={stackPresentation.isTop ? undefined : "true"}
			inert={stackPresentation.isTop ? undefined : true}
		>
			<button
				type="button"
				className="absolute inset-0 h-full w-full cursor-default bg-black/25 backdrop-blur-sm"
				aria-label="모달 닫기"
				tabIndex={-1}
				onMouseDown={(event) => {
					event.preventDefault();
				}}
				onClick={onClose}
			/>

			<div className="relative flex min-h-full items-center justify-center p-4">
				<div
					ref={dialogRef}
					role="dialog"
					aria-modal={stackPresentation.isTop ? "true" : undefined}
					aria-labelledby={titleId}
					tabIndex={-1}
					onKeyDown={handleKeyDown}
					onFocusCapture={(event) => {
						const entry = stackEntryRef.current;
						if (entry && event.target instanceof HTMLElement) {
							entry.lastFocusedInside = event.target;
						}
					}}
					className={clsx(
						"max-h-[calc(100dvh-2rem)] w-full overflow-y-auto overscroll-contain rounded-2xl border border-border-subtle bg-card p-6 text-left shadow-xl",
						maxWidthClasses[maxWidth],
					)}
				>
					<h3
						id={titleId}
						className={clsx(
							"text-lg font-bold leading-6 text-foreground",
							showHeaderBorder && "mb-4 border-b border-border-subtle pb-4",
							titleClassName,
						)}
					>
						{title}
					</h3>
					{children}
				</div>
			</div>
		</div>,
		portalHost,
	);
};
