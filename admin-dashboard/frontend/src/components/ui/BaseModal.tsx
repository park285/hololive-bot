import clsx from "clsx";
import { type ReactNode, useEffect } from "react";

interface BaseModalProps {
	isOpen: boolean;
	onClose: () => void;
	title?: ReactNode;
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

export const BaseModal = ({
	isOpen,
	onClose,
	title,
	children,
	maxWidth = "md",
	titleClassName,
	showHeaderBorder = false,
}: BaseModalProps) => {
	useEffect(() => {
		if (!isOpen) return;

		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape") {
				onClose();
			}
		};

		const previousOverflow = document.body.style.overflow;
		document.body.style.overflow = "hidden";
		window.addEventListener("keydown", onKeyDown);

		return () => {
			document.body.style.overflow = previousOverflow;
			window.removeEventListener("keydown", onKeyDown);
		};
	}, [isOpen, onClose]);

	if (!isOpen) {
		return null;
	}

	return (
		<div className="fixed inset-0 z-50">
			<button
				type="button"
				className="absolute inset-0 h-full w-full cursor-default bg-black/25 backdrop-blur-sm"
				aria-label="모달 닫기"
				onClick={onClose}
			/>

			<div className="relative flex min-h-full items-center justify-center p-4">
				<div
					role="dialog"
					aria-modal="true"
					className={clsx(
						"w-full overflow-hidden rounded-2xl border border-border-subtle bg-card p-6 text-left shadow-xl",
						maxWidthClasses[maxWidth],
					)}
				>
					{title && (
						<h3
							className={clsx(
								"text-lg font-bold leading-6 text-foreground",
								showHeaderBorder && "mb-4 border-b border-border-subtle pb-4",
								titleClassName,
							)}
						>
							{title}
						</h3>
					)}
					{children}
				</div>
			</div>
		</div>
	);
};
