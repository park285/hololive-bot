import CircleAlert from "lucide-react/dist/esm/icons/circle-alert.mjs";
import CheckCircle2 from "lucide-react/dist/esm/icons/circle-check-big.mjs";
import X from "lucide-react/dist/esm/icons/x.mjs";
import { useEffect, useState } from "react";
import toastApi, {
	getToastItems,
	subscribeToToasts,
	type ToastItem,
} from "@/lib/toast-api";

type ToastVariant = ToastItem["variant"];

interface ToasterProps {
	position?: "top-center";
	reverseOrder?: boolean;
	toastOptions?: {
		className?: string;
		success?: {
			iconTheme?: { primary?: string; secondary?: string };
		};
		error?: {
			iconTheme?: { primary?: string; secondary?: string };
		};
	};
}

const getVariantStyles = (
	variant: ToastVariant,
	toastOptions?: ToasterProps["toastOptions"],
) => {
	if (variant === "success") {
		return {
			color: toastOptions?.success?.iconTheme?.primary ?? "#0ea5e9",
			Icon: CheckCircle2,
		};
	}

	return {
		color: toastOptions?.error?.iconTheme?.primary ?? "#ef4444",
		Icon: CircleAlert,
	};
};

export const Toaster = ({
	position: _position = "top-center",
	reverseOrder = false,
	toastOptions,
}: ToasterProps) => {
	void _position;
	const [items, setItems] = useState<ToastItem[]>(getToastItems());

	useEffect(() => {
		const unsubscribe = subscribeToToasts((nextToasts: ToastItem[]) => {
			setItems(nextToasts);
		});
		return unsubscribe;
	}, []);

	if (items.length === 0) {
		return null;
	}

	const orderedItems = reverseOrder ? [...items].reverse() : items;
	const positionClassName = "top-4 left-1/2 -translate-x-1/2";

	return (
		<div
			className={`pointer-events-none fixed z-[100] flex w-full max-w-md flex-col gap-3 px-4 ${positionClassName}`}
		>
			{orderedItems.map((item) => {
				const { color, Icon } = getVariantStyles(item.variant, toastOptions);
				return (
					<div
						key={item.id}
						className={`pointer-events-auto flex items-start gap-3 rounded-xl border border-border-subtle bg-card px-4 py-3 text-card-foreground shadow-lg ${toastOptions?.className ?? ""}`}
						role="status"
						aria-live="polite"
					>
						<Icon
							size={18}
							style={{ color }}
							className="mt-0.5 shrink-0"
							aria-hidden="true"
						/>
						<div className="min-w-0 flex-1 text-sm">{item.message}</div>
						<button
							type="button"
							className="rounded p-1 text-subtle-foreground transition-colors hover:bg-muted hover:text-muted-foreground"
							onClick={() => {
								toastApi.dismiss(item.id);
							}}
							aria-label="알림 닫기"
						>
							<X size={14} aria-hidden="true" />
						</button>
					</div>
				);
			})}
		</div>
	);
};
