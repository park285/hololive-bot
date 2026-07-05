import clsx from "clsx";
import LogOut from "lucide-react/dist/esm/icons/log-out.mjs";
import Menu from "lucide-react/dist/esm/icons/menu.mjs";
import Play from "lucide-react/dist/esm/icons/play.mjs";
import X from "lucide-react/dist/esm/icons/x.mjs";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { authApi } from "@/api/core";
import { QueryErrorBoundary } from "@/components/QueryErrorBoundary";
import { Button } from "@/components/ui/Button";
import { ThemeToggle } from "@/components/ui/ThemeToggle";
import { broadcastSessionLogout } from "@/hooks/useActivityDetection";
import { clearClientSession } from "@/lib/sessionLifecycle";
import { NAV_GROUPS, prefetchRoute, ROUTE_MANIFEST } from "@/routes/manifest";

export const AppLayout = () => {
	const navigate = useNavigate();
	const location = useLocation();
	const [isSidebarOpen, setIsSidebarOpen] = useState(true);
	const [isMobileNavOpen, setIsMobileNavOpen] = useState(false);
	const [isLoggingOut, setIsLoggingOut] = useState(false);
	const [isDesktop, setIsDesktop] = useState(() =>
		typeof window !== "undefined" && typeof window.matchMedia === "function"
			? window.matchMedia("(min-width: 768px)").matches
			: true,
	);
	const asideRef = useRef<HTMLElement>(null);
	const menuButtonRef = useRef<HTMLButtonElement>(null);

	const handleLogout = () => {
		if (isLoggingOut) {
			return;
		}

		setIsLoggingOut(true);
		void (async () => {
			try {
				await authApi.logout();
			} catch {
				// 서버 로그아웃 실패 여부와 무관하게 클라이언트 세션은 정리합니다.
			} finally {
				broadcastSessionLogout();
				setIsLoggingOut(false);
				clearClientSession();
				void navigate("/login", { replace: true });
			}
		})();
	};

	useEffect(() => {
		if (typeof window.matchMedia !== "function") {
			return;
		}
		const query = window.matchMedia("(min-width: 768px)");
		const onChange = (event: MediaQueryListEvent) => {
			setIsDesktop(event.matches);
		};
		setIsDesktop(query.matches);
		query.addEventListener("change", onChange);
		return () => {
			query.removeEventListener("change", onChange);
		};
	}, []);

	useEffect(() => {
		setIsMobileNavOpen(false);
	}, [location.pathname]);

	useEffect(() => {
		if (!isMobileNavOpen) {
			return;
		}
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape") {
				setIsMobileNavOpen(false);
			}
		};
		window.addEventListener("keydown", onKeyDown);
		return () => {
			window.removeEventListener("keydown", onKeyDown);
		};
	}, [isMobileNavOpen]);

	const mobileDrawerOpen = !isDesktop && isMobileNavOpen;
	const wasDrawerOpen = useRef(false);

	useEffect(() => {
		if (mobileDrawerOpen) {
			asideRef.current?.querySelector<HTMLElement>("nav a[href]")?.focus();
		} else if (wasDrawerOpen.current) {
			menuButtonRef.current?.focus();
		}
		wasDrawerOpen.current = mobileDrawerOpen;
	}, [mobileDrawerOpen]);

	const expanded = isDesktop ? isSidebarOpen : true;

	const navGroups = NAV_GROUPS;

	const activeRoute = ROUTE_MANIFEST.find((r) => {
		const routePath = r.absolutePath;
		if (location.pathname === routePath) return true;
		if (location.pathname.startsWith(`${routePath}/`)) return true;
		return false;
	});

	return (
		<div className="flex h-screen bg-background overflow-hidden font-body selection:bg-sky-200">
			<div className="absolute inset-0 z-0 pointer-events-none">
				<div className="absolute top-0 left-0 w-full h-96 bg-linear-to-b from-sky-50/50 to-transparent dark:from-sky-950/30"></div>
				<div
					className="absolute inset-0 opacity-[0.012]"
					style={{
						backgroundImage:
							"url(\"data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
					}}
				/>
			</div>

			{isMobileNavOpen && (
				<button
					type="button"
					aria-label="메뉴 닫기"
					onClick={() => {
						setIsMobileNavOpen(false);
					}}
					className="md:hidden fixed inset-0 z-30 bg-black/40 backdrop-blur-sm"
				/>
			)}

			<aside
				ref={asideRef}
				id="app-sidebar"
				inert={!isDesktop && !isMobileNavOpen}
				role={mobileDrawerOpen ? "dialog" : undefined}
				aria-modal={mobileDrawerOpen || undefined}
				aria-label="주 내비게이션"
				className={clsx(
					"fixed inset-y-0 left-0 z-40 w-[260px] flex flex-col bg-card/80 backdrop-blur-xl border-r border-border shadow-sm transition-transform duration-300 md:relative md:translate-x-0 md:transition-[width]",
					isMobileNavOpen ? "translate-x-0" : "-translate-x-full",
					isSidebarOpen ? "md:w-[260px]" : "md:w-20",
				)}
			>
				<div className="h-20 flex items-center justify-between px-6 border-b border-border-subtle">
					{expanded ? (
						<div className="flex items-center gap-3">
							<div className="w-8 h-8 bg-linear-to-br from-sky-400 to-cyan-400 rounded-lg flex items-center justify-center shadow-md shadow-sky-200">
								<Play className="w-4 h-4 text-white fill-white ml-0.5" />
							</div>
							<span className="text-lg font-display font-bold text-foreground tracking-tight">
								관리자 콘솔
							</span>
						</div>
					) : (
						<div className="mx-auto w-8 h-8 bg-linear-to-br from-sky-400 to-cyan-400 rounded-lg flex items-center justify-center shadow-md shadow-sky-200">
							<Play className="w-4 h-4 text-white fill-white ml-0.5" />
						</div>
					)}
					{expanded && (
						<Button
							type="button"
							variant="ghost"
							size="icon"
							onClick={() => {
								setIsSidebarOpen(false);
							}}
							className="hidden md:inline-flex rounded-lg text-subtle-foreground hover:text-muted-foreground"
							aria-label="사이드바 닫기"
						>
							<X />
						</Button>
					)}
				</div>

				{!expanded && (
					<div className="py-4 flex justify-center border-b border-border-subtle">
						<Button
							type="button"
							variant="ghost"
							size="icon"
							onClick={() => {
								setIsSidebarOpen(true);
							}}
							className="rounded-lg text-subtle-foreground hover:text-muted-foreground"
							aria-label="사이드바 열기"
						>
							<Menu />
						</Button>
					</div>
				)}

					<nav className="flex-1 py-6 px-3 overflow-y-auto scrollbar-hide animate-fade-in">
						{navGroups.map((group, groupIndex) => (
							<div
								key={group.title}
								className="mb-6 last:mb-0 animate-slide-in-left"
								style={{ animationDelay: `${String(groupIndex * 80)}ms` }}
							>
								{expanded && (
									<div className="px-3 mb-2">
										<h3 className="text-xs font-semibold text-subtle-foreground uppercase tracking-wider">
											{group.title}
									</h3>
								</div>
							)}
							<div className="space-y-1">
								{group.items.map((item) => (
									<NavLink
										key={item.id}
										to={item.absolutePath}
										className={({ isActive }) =>
											clsx(
												"flex items-center px-3 py-3.5 rounded-xl transition-all duration-200 group relative overflow-hidden",
												isActive
													? "bg-linear-to-r from-sky-500 to-cyan-500 text-white shadow-lg shadow-sky-300/40 scale-[1.02]"
													: "text-muted-foreground hover:bg-accent hover:text-foreground",
											)
										}
										title={!expanded ? item.label : undefined}
										aria-label={item.label}
										onMouseEnter={() => {
											prefetchRoute(item.id);
										}}
									>
										{({ isActive }) => (
											<>
												<item.icon
													size={22}
													strokeWidth={isActive ? 2.5 : 2}
													className={clsx(
														"shrink-0 transition-colors",
														isActive
															? "text-white"
															: "text-subtle-foreground group-hover:text-muted-foreground",
													)}
												/>
												{expanded && (
													<span className="ml-3 font-medium whitespace-nowrap">
														{item.label}
													</span>
												)}
												{isActive && (
													<div className="absolute left-0 top-1/2 -translate-y-1/2 w-1 h-8 rounded-r-full bg-linear-to-b from-white to-cyan-200/50 dark:from-slate-800 dark:to-cyan-900/40" />
												)}
											</>
										)}
									</NavLink>
								))}
							</div>
						</div>
					))}
				</nav>

				<div className="p-4 border-t border-border-subtle">
					<Button
						type="button"
						variant="ghost"
						fullWidth
						onClick={handleLogout}
						disabled={isLoggingOut}
						className={clsx(
							"group h-auto gap-0 p-3.5 rounded-xl text-muted-foreground hover:bg-rose-50 hover:text-rose-600",
							expanded ? "justify-start" : "justify-center",
						)}
						aria-label="로그아웃"
					>
						<LogOut
							size={20}
							className="group-hover:stroke-rose-600 transition-colors"
						/>
						{expanded && (
							<span className="ml-3 font-medium">
									{isLoggingOut ? "로그아웃 중…" : "로그아웃"}
								</span>
						)}
					</Button>
				</div>
			</aside>

			<main
				inert={mobileDrawerOpen}
				className="flex-1 flex flex-col min-w-0 overflow-hidden relative z-10"
			>
				<header className="h-20 bg-card/60 backdrop-blur-md border-b border-border/50 flex items-center justify-between px-4 md:px-8 sticky top-0 z-20">
					<div className="flex items-center gap-3 min-w-0">
						<Button
							ref={menuButtonRef}
							type="button"
							variant="ghost"
							size="icon"
							className="md:hidden shrink-0"
							onClick={() => {
								setIsMobileNavOpen((prev) => !prev);
							}}
							aria-label="메뉴 열기"
							aria-expanded={mobileDrawerOpen}
							aria-controls="app-sidebar"
						>
							<Menu aria-hidden="true" />
						</Button>
						<div className="animate-fade-in-up flex items-center gap-3 min-w-0">
							<div className="w-1 h-10 rounded-full bg-linear-to-b from-sky-400 to-cyan-400 shrink-0" />
							<div className="min-w-0">
								<h2 className="text-2xl font-display font-bold text-foreground tracking-tight truncate">
									{activeRoute?.label || "대시보드"}
								</h2>
								<p className="text-xs text-subtle-foreground font-medium mt-0.5 tracking-wide hidden sm:block">
									통합 봇 관리 시스템
								</p>
							</div>
						</div>
					</div>

					<div className="flex items-center space-x-4 shrink-0">
						<ThemeToggle />
						<div className="flex items-center space-x-3 px-1 py-1 bg-card border border-border rounded-full shadow-sm pr-4">
							<div className="w-8 h-8 rounded-full bg-linear-to-tr from-sky-400 to-cyan-400 flex items-center justify-center text-white font-bold text-sm shadow-sm ring-2 ring-white">
								A
							</div>
							<div className="flex flex-col justify-center">
								<span className="text-sm font-display font-bold text-foreground leading-none">
									관리자
								</span>
							</div>
						</div>
					</div>
				</header>

				<div className="flex-1 overflow-auto p-6 sm:p-10 scroll-smooth">
					<div className="max-w-7xl mx-auto w-full">
						<QueryErrorBoundary>
							<Outlet />
						</QueryErrorBoundary>
					</div>
				</div>
			</main>
		</div>
	);
};
