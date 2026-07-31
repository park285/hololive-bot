export interface ModalStackPresentation {
	zIndex: number;
	isTop: boolean;
}

export interface LayeredModalStackEntry {
	setStackPresentation: (presentation: ModalStackPresentation) => void;
}

export const baseModalZIndex = 50;

const syncModalStackPresentation = (stack: LayeredModalStackEntry[]) => {
	const topIndex = stack.length - 1;
	stack.forEach((entry, index) => {
		entry.setStackPresentation({
			zIndex: baseModalZIndex + index,
			isTop: index === topIndex,
		});
	});
};

export const addModalStackEntry = <T extends LayeredModalStackEntry>(
	stack: T[],
	entry: T,
) => {
	stack.push(entry);
	syncModalStackPresentation(stack);
};

export const removeModalStackEntry = <T extends LayeredModalStackEntry>(
	stack: T[],
	entry: T,
) => {
	const index = stack.indexOf(entry);
	if (index < 0) {
		return { removed: false, wasTop: false, nextTop: undefined };
	}

	const wasTop = index === stack.length - 1;
	stack.splice(index, 1);
	syncModalStackPresentation(stack);

	return {
		removed: true,
		wasTop,
		nextTop: stack.at(-1),
	};
};
