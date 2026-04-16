import type { VariantProps } from "class-variance-authority";
import X from "lucide-react/dist/esm/icons/x";
import type * as React from "react";
import { cn } from "@/lib/utils";
import {
	type BadgeVariant,
	badgeVariants,
	isValidBadgeVariant,
} from "./badge-variants";

export interface BadgeProps
	extends React.HTMLAttributes<HTMLDivElement>,
		VariantProps<typeof badgeVariants> {
	/** 레거시 호환성을 위한 color prop (variant 우선) */
	color?: BadgeVariant;
	onRemove?: () => void;
}

function Badge({
	className,
	variant,
	color,
	onRemove,
	children,
	...props
}: BadgeProps) {
	let finalVariant: BadgeVariant = "default";
	if (variant) {
		finalVariant = variant;
	} else if (color && isValidBadgeVariant(color)) {
		finalVariant = color;
	}

	return (
		<div
			className={cn(badgeVariants({ variant: finalVariant }), className)}
			{...props}
		>
			{children}
			{onRemove && (
				<button
					onClick={onRemove}
					className="ml-1 group rounded-full p-0.5 hover:bg-black/5 transition-colors"
					type="button"
					aria-label="삭제"
				>
					<X
						size={12}
						strokeWidth={3}
						className="opacity-60 group-hover:opacity-100"
					/>
				</button>
			)}
		</div>
	);
}

export { Badge };
