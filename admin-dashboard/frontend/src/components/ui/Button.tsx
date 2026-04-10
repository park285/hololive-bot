import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
	"inline-flex cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
	{
		variants: {
			variant: {
				default:
					"bg-primary text-primary-foreground shadow hover:bg-primary/90",
				destructive:
					"bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90",
				outline:
					"border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground",
				secondary:
					"bg-secondary text-secondary-foreground shadow-sm hover:bg-secondary/80",
				ghost: "hover:bg-accent hover:text-accent-foreground",
				link: "text-primary underline-offset-4 hover:underline",
			},
			size: {
				default: "h-9 px-4 py-2",
				sm: "h-8 rounded-md px-3 text-xs",
				lg: "h-10 rounded-md px-8",
				icon: "h-9 w-9",
			},
		},
		defaultVariants: {
			variant: "default",
			size: "default",
		},
	},
);

const buttonVariantValues = [
	"default",
	"destructive",
	"outline",
	"secondary",
	"ghost",
	"link",
] as const;
type ButtonVariantValue = (typeof buttonVariantValues)[number];
const isButtonVariantValue = (value: string): value is ButtonVariantValue =>
	buttonVariantValues.some((variant) => variant === value);

const buttonSizeValues = ["default", "sm", "lg", "icon"] as const;
type ButtonSizeValue = (typeof buttonSizeValues)[number];
const isButtonSizeValue = (value: string): value is ButtonSizeValue =>
	buttonSizeValues.some((size) => size === value);

export interface ButtonProps
	extends React.ButtonHTMLAttributes<HTMLButtonElement>,
		Omit<VariantProps<typeof buttonVariants>, "variant" | "size"> {
	fullWidth?: boolean;
	variant?:
		| "default"
		| "destructive"
		| "outline"
		| "secondary"
		| "ghost"
		| "link"
		| "primary"
		| "danger"
		| null;
	size?: "default" | "sm" | "lg" | "icon" | "md" | null;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
	(
		{
			className,
			variant = "default",
			size = "default",
			fullWidth = false,
			...props
		},
		ref,
	) => {
		let effectiveVariant: ButtonVariantValue = "default";
		if (variant === "primary") effectiveVariant = "default";
		else if (variant === "danger") effectiveVariant = "destructive";
		else if (variant && isButtonVariantValue(variant))
			effectiveVariant = variant;

		let effectiveSize: ButtonSizeValue = "default";
		if (size === "md") effectiveSize = "default";
		else if (size && isButtonSizeValue(size)) effectiveSize = size;

		return (
			<button
				className={cn(
					buttonVariants({
						variant: effectiveVariant,
						size: effectiveSize,
						className,
					}),
					fullWidth && "w-full",
				)}
				ref={ref}
				{...props}
			/>
		);
	},
);
Button.displayName = "Button";

export { Button };
