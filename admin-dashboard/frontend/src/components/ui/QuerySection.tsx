import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { getErrorMessageFromUnknown } from "@/lib/typeUtils";

interface QuerySectionProps {
	isLoading: boolean;
	isError: boolean;
	error?: unknown;
	onRetry?: () => void;
	isEmpty?: boolean;
	skeleton?: ReactNode;
	emptyContent?: ReactNode;
	children: ReactNode;
}

const DefaultSkeleton = () => (
	<div className="flex items-center justify-center h-48 text-subtle-foreground">
		<Loader2 className="w-6 h-6 animate-spin mr-2" />
		<span className="text-sm">불러오는 중…</span>
	</div>
);

export const QuerySection = ({
	isLoading,
	isError,
	error,
	onRetry,
	isEmpty = false,
	skeleton,
	emptyContent,
	children,
}: QuerySectionProps) => {
	if (isLoading) {
		return <>{skeleton ?? <DefaultSkeleton />}</>;
	}

	if (isError) {
		return (
			<div className="rounded-2xl border border-border-subtle bg-card p-8 text-center">
				<p className="font-bold text-destructive">
					데이터를 불러오지 못했습니다.
				</p>
				<p className="mt-2 text-sm text-muted-foreground">
					{getErrorMessageFromUnknown(error)}
				</p>
				{onRetry ? (
					<Button
						variant="destructive"
						size="sm"
						className="mt-4"
						onClick={onRetry}
					>
						다시 시도
					</Button>
				) : null}
			</div>
		);
	}

	if (isEmpty) {
		return <>{emptyContent}</>;
	}

	return <>{children}</>;
};
