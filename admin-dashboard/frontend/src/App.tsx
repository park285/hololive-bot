import { QueryClientProvider } from "@tanstack/react-query";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import { lazy, Suspense, useCallback } from "react";
import {
	createBrowserRouter,
	Navigate,
	RouterProvider,
} from "react-router-dom";
import { SessionAbsoluteWarningModal } from "@/components/auth/SessionAbsoluteWarningModal";
import { SessionIdleWarningModal } from "@/components/auth/SessionIdleWarningModal";
import { QueryErrorBoundary } from "@/components/QueryErrorBoundary";
import { CONFIG } from "@/config";
import { useActivityDetection } from "@/hooks/useActivityDetection";
import { useAuthBootstrap } from "@/hooks/useAuthBootstrap";
import { useHeartbeat } from "@/hooks/useHeartbeat";
import { useSessionWarnings } from "@/hooks/useSessionWarnings";
import { queryClient } from "@/lib/queryClient";
import { Toaster } from "@/lib/toast";
import {
	getLazyComponent,
	ROUTE_DEFINITIONS,
} from "@/routes/route-definitions";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

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
	const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
	const isAuthResolved = useAuthStore((state) => state.isAuthResolved);
	const logout = useAuthStore((state) => state.logout);
	const policy = useSessionWarningStore((state) => state.policy);
	const idleTimeoutMs =
		policy?.idle_timeout_ms ?? CONFIG.heartbeat.idleTimeoutMs;
	const activityEnabled = isAuthResolved && isAuthenticated;
	const handleRemoteLogout = useCallback(() => {
		logout();
		queryClient.clear();
	}, [logout]);
	const isIdle = useActivityDetection({
		enabled: activityEnabled,
		idleTimeoutMs,
		onRemoteLogout: handleRemoteLogout,
	});

	useHeartbeat(isIdle);
	useSessionWarnings(isIdle);

	if (!isAuthResolved) {
		return <FullPageLoader />;
	}

	if (!isAuthenticated) {
		return <Navigate to="/login" replace />;
	}

	return (
		<>
			{children}
			<SessionIdleWarningModal />
			<SessionAbsoluteWarningModal />
		</>
	);
};

const AuthBootstrap = () => {
	useAuthBootstrap();
	return null;
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

const App = () => (
	<QueryClientProvider client={queryClient}>
		<AuthBootstrap />
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

export default App;
