import { cva, type VariantProps } from "class-variance-authority";

export const badgeVariants = cva(
	"inline-flex items-center rounded-md px-2.5 py-1 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
	{
		variants: {
			variant: {
				default:
					"border-transparent bg-primary text-primary-foreground hover:bg-primary/80",
				secondary:
					"border-transparent bg-muted text-foreground hover:bg-border",
				destructive:
					"border-transparent bg-destructive text-destructive-foreground hover:bg-destructive/80",
				outline: "text-foreground border border-border",
				blue: "border-transparent bg-blue-50 text-blue-700 ring-1 ring-inset ring-blue-700/10 dark:bg-blue-950/40 dark:text-blue-300 dark:ring-blue-400/20",
				sky: "border-transparent bg-sky-50 text-sky-700 ring-1 ring-inset ring-sky-700/10 dark:bg-sky-950/40 dark:text-sky-300 dark:ring-sky-400/20",
				indigo:
					"border-transparent bg-indigo-50 text-indigo-700 ring-1 ring-inset ring-indigo-700/10 dark:bg-indigo-950/40 dark:text-indigo-300 dark:ring-indigo-400/20",
				green:
					"border-transparent bg-emerald-50 text-emerald-700 ring-1 ring-inset ring-emerald-700/10 dark:bg-emerald-950/40 dark:text-emerald-300 dark:ring-emerald-400/20",
				yellow:
					"border-transparent bg-yellow-50 text-yellow-800 ring-1 ring-inset ring-yellow-600/20 dark:bg-yellow-950/40 dark:text-yellow-300 dark:ring-yellow-400/20",
				amber:
					"border-transparent bg-amber-50 text-amber-700 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-950/40 dark:text-amber-300 dark:ring-amber-400/20",
				red: "border-transparent bg-red-50 text-red-700 ring-1 ring-inset ring-red-600/10 dark:bg-red-950/40 dark:text-red-300 dark:ring-red-400/20",
				rose: "border-transparent bg-rose-50 text-rose-700 ring-1 ring-inset ring-rose-600/10 dark:bg-rose-950/40 dark:text-rose-300 dark:ring-rose-400/20",
				gray: "border-transparent bg-muted text-muted-foreground ring-1 ring-inset ring-border",
			},
		},
		defaultVariants: {
			variant: "default",
		},
	},
);

export type BadgeVariant = NonNullable<
	VariantProps<typeof badgeVariants>["variant"]
>;

export const validBadgeVariants = [
	"default",
	"secondary",
	"destructive",
	"outline",
	"blue",
	"sky",
	"indigo",
	"green",
	"yellow",
	"amber",
	"red",
	"rose",
	"gray",
] as const;

export const isValidBadgeVariant = (value: string): value is BadgeVariant =>
	(validBadgeVariants as readonly string[]).includes(value);
