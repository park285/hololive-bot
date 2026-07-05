import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle.mjs";
import RefreshCw from "lucide-react/dist/esm/icons/refresh-cw.mjs";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { queryClient } from "@/lib/queryClient";

interface ErrorBoundaryProps {
	children: ReactNode;
	fallback?: ReactNode;
	onError?: (error: Error, errorInfo: ErrorInfo) => void;
}

interface ErrorBoundaryState {
	hasError: boolean;
	error: Error | null;
}

export class QueryErrorBoundary extends Component<
	ErrorBoundaryProps,
	ErrorBoundaryState
> {
	constructor(props: ErrorBoundaryProps) {
		super(props);
		this.state = { hasError: false, error: null };
	}

	static getDerivedStateFromError(error: Error): ErrorBoundaryState {
		return { hasError: true, error };
	}

	componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
		console.error("QueryErrorBoundary caught an error:", error, errorInfo);
		this.props.onError?.(error, errorInfo);
	}

	handleRetry = (): void => {
		void queryClient.resetQueries();
		this.setState({ hasError: false, error: null });
	};

	render(): ReactNode {
		if (this.state.hasError) {
			if (this.props.fallback) {
				return this.props.fallback;
			}

			return (
				<div className="flex flex-col items-center justify-center min-h-[300px] p-8 rounded-2xl border border-border/60 bg-card/50 backdrop-blur-sm shadow-sm">
					<div className="w-14 h-14 bg-rose-50 rounded-full flex items-center justify-center mb-5 ring-4 ring-rose-50/50">
						<AlertTriangle
							className="w-7 h-7 text-rose-500"
							strokeWidth={1.5}
						/>
					</div>
					<h3 className="text-lg font-bold text-foreground mb-2 tracking-tight">
						문제가 발생했습니다
					</h3>
					<p className="text-sm text-muted-foreground mb-6 text-center max-w-md leading-relaxed break-keep">
						{this.state.error?.message ?? "알 수 없는 오류가 발생했습니다."}
					</p>
					<Button
						onClick={this.handleRetry}
						variant="outline"
						className="gap-2 pl-3 pr-4 h-10 shadow-sm hover:border-slate-300 dark:hover:border-slate-600 hover:bg-card transition-all"
					>
						<RefreshCw size={16} />
						다시 시도
					</Button>
				</div>
			);
		}

		return this.props.children;
	}
}

export const QueryLoadingFallback = () => (
	<div className="flex items-center justify-center min-h-[200px] text-subtle-foreground">
		<RefreshCw className="w-5 h-5 animate-spin mr-2" />
		<span className="text-sm font-medium">로딩 중…</span>
	</div>
);
