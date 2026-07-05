export const ChartSkeleton = ({ label }: { label: string }) => (
	<div className="absolute inset-0 z-10 flex items-center justify-center rounded-b-lg bg-muted/50">
		<div className="flex items-center gap-2">
			<div className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-sky-500 dark:border-slate-600 dark:border-t-sky-500" />
			<span className="text-xs text-muted-foreground">{label}</span>
		</div>
	</div>
);
