import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";

interface ConfirmModalProps {
	isOpen: boolean;
	onClose: () => void;
	onConfirm: () => void;
	title: string;
	message: React.ReactNode;
	confirmText?: string;
	confirmColor?: "primary" | "danger";
	isPending?: boolean;
	children?: React.ReactNode;
}

export function ConfirmModal({
	isOpen,
	onClose,
	onConfirm,
	title,
	message,
	confirmText = "확인",
	confirmColor = "primary",
	isPending = false,
	children,
}: ConfirmModalProps) {
	const buttonVariant = confirmColor === "danger" ? "destructive" : "default";
	const handleClose = () => {
		if (!isPending) onClose();
	};

	return (
		<BaseModal isOpen={isOpen} onClose={handleClose} title={title}>
			<div className="mt-2">
				<div className="text-sm text-muted-foreground whitespace-pre-wrap">
					{message}
				</div>
				{children && <div className="mt-4">{children}</div>}
			</div>

			<div className="mt-6 flex justify-end gap-3">
				<Button
					type="button"
					variant="outline"
					onClick={handleClose}
					disabled={isPending}
				>
					취소
				</Button>
				<Button
					type="button"
					variant={buttonVariant}
					onClick={onConfirm}
					disabled={isPending}
					aria-busy={isPending}
				>
					<span aria-live="polite">{confirmText}</span>
				</Button>
			</div>
		</BaseModal>
	);
}
