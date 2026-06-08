import CircleAlert from "lucide-react/dist/esm/icons/circle-alert.mjs";
import CheckCircle2 from "lucide-react/dist/esm/icons/circle-check-big.mjs";
import X from "lucide-react/dist/esm/icons/x.mjs";
import { type CSSProperties, useEffect, useState } from "react";
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
		style?: CSSProperties;
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
						className={`pointer-events-auto flex items-start gap-3 rounded-xl border bg-white p-4 shadow-lg ${toastOptions?.className ?? ""}`}
						style={toastOptions?.style}
						role="status"
						aria-live="polite"
					>
						<Icon
							size={18}
							style={{ color }}
							className="mt-0.5 shrink-0"
							aria-hidden="true"
						/>
						<div className="min-w-0 flex-1 text-sm text-slate-700">
							{item.message}
						</div>
						<button
							type="button"
							className="rounded p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
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
