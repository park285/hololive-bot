import { useVirtualizer } from "@tanstack/react-virtual";
import {
	type AriaRole,
	type Key,
	type ReactNode,
	useEffect,
	useRef,
} from "react";
import { cn } from "@/lib/utils";

interface VirtualListProps<T> {
	items: T[];
	estimateSize: (index: number) => number;
	renderItem: (item: T, index: number) => ReactNode;
	getItemKey?: (item: T, index: number) => Key;
	overscan?: number;
	recomputeKey?: unknown;
	className?: string;
	contentClassName?: string;
	itemClassName?: string;
	role?: AriaRole;
}

export const VirtualList = <T,>({
	items,
	estimateSize,
	renderItem,
	getItemKey,
	overscan = 6,
	recomputeKey,
	className,
	contentClassName,
	itemClassName,
	role = "list",
}: VirtualListProps<T>) => {
	const scrollRef = useRef<HTMLDivElement>(null);
	const virtualizer = useVirtualizer({
		count: items.length,
		getScrollElement: () => scrollRef.current,
		estimateSize,
		overscan,
		measureElement: (element) => element.getBoundingClientRect().height,
	});

	useEffect(() => {
		virtualizer.measure();
	}, [items.length, recomputeKey, virtualizer]);

	return (
		<div
			ref={scrollRef}
			className={cn("overflow-auto", className)}
			role={role}
		>
			<div
				className={cn("relative w-full", contentClassName)}
				style={{ height: `${String(virtualizer.getTotalSize())}px` }}
			>
				{virtualizer.getVirtualItems().map((virtualItem) => {
					const item = items[virtualItem.index];
					if (item === undefined) {
						return null;
					}

					return (
						<div
							key={getItemKey?.(item, virtualItem.index) ?? virtualItem.key}
							ref={virtualizer.measureElement}
							data-index={virtualItem.index}
							className={cn("absolute left-0 top-0 w-full", itemClassName)}
							style={{ transform: `translateY(${String(virtualItem.start)}px)` }}
						>
							{renderItem(item, virtualItem.index)}
						</div>
					);
				})}
			</div>
		</div>
	);
};
