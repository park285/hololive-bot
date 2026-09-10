import { AuthenticatedSession } from "@/session/AuthenticatedSession";
import { useSessionSnapshot } from "@/session/useSession";
import { QueryClientProvider } from "@tanstack/react-query";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import { lazy, Suspense, useSyncExternalStore } from "react";
import {
	createBrowserRouter,
	Navigate,
} from "react-router";
import { RouterProvider } from "react-router/dom";
import { bootstrapApplication, generation } from "@/app/bootstrap";
import { QueryErrorBoundary } from "@/components/QueryErrorBoundary";
import { queryClient } from "@/queries/client";
import { Toaster } from "@/lib/toast";
import {
	getLazyComponent,
	ROUTE_DEFINITIONS,
} from "@/routes/route-definitions";

const LoginPage = lazy(() => import("@/pages/LoginPage"));
const AppLayout = lazy(() =>
	import("@/layouts/AppLayout").then((module) => ({
		default: module.AppLayout,
	})),
);
const ErrorPage = lazy(() => import("@/components/ErrorPage"));
const ReactQueryDevtools = import.meta.env.DEV
	? lazy(() =>
			import("@tanstack/react-query-devtools").then((module) => ({
				default: module.ReactQueryDevtools,
			})),
		)
	: null;

const TabLoader = () => (
	<div className="flex h-64 items-center justify-center text-subtle-foreground">
		<div className="animate-spin mr-2">
			<Loader2 className="h-6 w-6" />
		</div>
		<span className="text-sm font-medium">로딩 중…</span>
	</div>
);

const FullPageLoader = () => (
	<div className="flex min-h-screen items-center justify-center bg-background text-subtle-foreground">
		<div className="animate-spin mr-2">
			<Loader2 className="h-6 w-6" />
		</div>
		<span className="text-sm font-medium">페이지를 준비 중…</span>
	</div>
);

const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
	const auth = useSessionSnapshot();
	if (auth.phase === "pending") return <FullPageLoader />;
	if (auth.phase === "signed_out") return <Navigate to="/login" replace />;
	// 중간 pending 렌더가 묶여도 인증 세대 변경은 로컬 modal·timer·편집 상태를 폐기합니다.
	return <AuthenticatedSession key={auth.authGeneration} policy={auth.policy}>{children}</AuthenticatedSession>;
};

const LazyRoute = ({ children }: { children: React.ReactNode }) => (
	<Suspense fallback={<TabLoader />}>{children}</Suspense>
);

const LoginRoute = () => (
	<Suspense fallback={<FullPageLoader />}>
		<LoginPage />
	</Suspense>
);

const DashboardShellRoute = () => (
	<Suspense fallback={<FullPageLoader />}>
		<AppLayout />
	</Suspense>
);

const RouteErrorElement = () => (
	<Suspense fallback={<FullPageLoader />}>
		<ErrorPage />
	</Suspense>
);

const router = createBrowserRouter([
	{
		path: "/login",
		element: <LoginRoute />,
		errorElement: <RouteErrorElement />,
	},
	{
		path: "/dashboard",
		element: (
			<ProtectedRoute>
				<DashboardShellRoute />
			</ProtectedRoute>
		),
		errorElement: <RouteErrorElement />,
		children: [
			{
				index: true,
				element: <Navigate to="stats" replace />,
			},
			...ROUTE_DEFINITIONS.map((route) => {
				const Component = getLazyComponent(route.id);
				return {
					path: route.path,
					element: (
						<LazyRoute>
							<Component />
						</LazyRoute>
					),
				};
			}),
		],
	},
	{
		path: "/",
		element: <Navigate to="/dashboard" replace />,
		errorElement: <RouteErrorElement />,
	},
	{
		path: "*",
		element: <Navigate to="/dashboard" replace />,
	},
]);

const toastOptions = {
	className: "text-sm font-medium",
	success: {
		iconTheme: { primary: "#0ea5e9", secondary: "#ffffff" },
	},
	error: {
		iconTheme: { primary: "#ef4444", secondary: "#ffffff" },
	},
};

const ReadyApp = () => (
	<QueryClientProvider client={queryClient}>
		<Toaster
			position="top-center"
			reverseOrder={false}
			toastOptions={toastOptions}
		/>
		<QueryErrorBoundary>
			<RouterProvider router={router} />
		</QueryErrorBoundary>
		{ReactQueryDevtools && (
			<Suspense fallback={null}>
				<ReactQueryDevtools initialIsOpen={false} buttonPosition="bottom-left" />
			</Suspense>
		)}
	</QueryClientProvider>
);

const App = () => {
	const state = useSyncExternalStore(generation.subscribe, generation.snapshot);
	if (state.phase === "ready") return <ReadyApp />;
	return <main className="min-h-screen flex flex-col items-center justify-center gap-4 bg-background p-6 text-foreground">
		<p role={state.phase === "checking" ? "status" : "alert"}>{state.message}</p>
		{state.phase === "incompatible" && <button className="rounded-lg bg-primary px-4 py-2 text-primary-foreground" onClick={() => { window.location.reload(); }}>새로고침</button>}
		{state.phase === "unavailable" && <button className="rounded-lg bg-primary px-4 py-2 text-primary-foreground" onClick={() => { void bootstrapApplication(); }}>다시 확인</button>}
	</main>;
};

export default App;
