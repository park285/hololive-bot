export interface SectionQueryLike {
	isLoading: boolean;
	isError: boolean;
	error: unknown;
	refetch: () => unknown;
}

export interface SectionStateProps {
	isLoading: boolean;
	isError: boolean;
	error: unknown;
	onRetry: () => void;
}

export const sectionStateProps = (
	query: SectionQueryLike,
): SectionStateProps => ({
	isLoading: query.isLoading,
	isError: query.isError,
	error: query.error,
	onRetry: () => {
		void query.refetch();
	},
});
