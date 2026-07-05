import * as React from "react";
import { cn } from "@/lib/utils";

const Progress = React.forwardRef<
	HTMLDivElement,
	React.HTMLAttributes<HTMLDivElement> & { value?: number | null }
>(({ className, value, ...props }, ref) => (
	<div
		ref={ref}
		className={cn(
			"relative h-4 w-full overflow-hidden rounded-full bg-muted",
			className,
		)}
		{...props}
	>
		<div
			className="h-full w-full flex-1 bg-linear-to-r from-amber-400 to-amber-500 transition-all"
			style={{ transform: `translateX(-${String(100 - (value || 0))}%)` }}
		/>
	</div>
));
Progress.displayName = "Progress";

export { Progress };
