import clsx from "clsx";
import LogOut from "lucide-react/dist/esm/icons/log-out";
import Menu from "lucide-react/dist/esm/icons/menu";
import Play from "lucide-react/dist/esm/icons/play";
import X from "lucide-react/dist/esm/icons/x";
import { useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { authApi } from "@/api/core";
import { QueryErrorBoundary } from "@/components/QueryErrorBoundary";
import { getNavGroups, prefetchRoute, ROUTE_MANIFEST } from "@/routes/manifest";
import { useAuthStore } from "@/stores/authStore";

export const AppLayout = () => {
	const navigate = useNavigate();
	const location = useLocation();
	const logout = useAuthStore((state) => state.logout);
	const [isSidebarOpen, setIsSidebarOpen] = useState(true);

	const handleLogout = () => {
		void (async () => {
			try {
				await authApi.logout();
			} catch {
				// 에러 무시
			}
			logout();
			queryClient.clear();
			void navigate("/login");
		})();
	};

	const navGroups = getNavGroups();

	// Find the current active route label
	const activeRoute = ROUTE_MANIFEST.find((r) => {
		const routePath = r.absolutePath; // e.g., /dashboard/stats
		// Handle exact match or parent match for nested routes
		if (location.pathname === routePath) return true;
		if (location.pathname.startsWith(`${routePath}/`)) return true;
		return false;
	});

	return (
		<div className="flex h-screen bg-slate-50 overflow-hidden font-body selection:bg-sky-200">
			{/* 동적 배경 */}
			<div className="absolute inset-0 z-0 pointer-events-none">
				<div className="absolute top-0 left-0 w-full h-96 bg-linear-to-b from-sky-50/50 to-transparent"></div>
				<div
					className="absolute inset-0 opacity-[0.012]"
					style={{
						backgroundImage:
							"url(\"data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
					}}
				/>
			</div>

			{/* 사이드바 */}
			<aside
				style={{ width: isSidebarOpen ? 260 : 80 }}
				className="bg-white/80 backdrop-blur-xl border-r border-slate-200 z-20 flex flex-col transition-[width] duration-300 relative shadow-sm"
			>
				{/* 로고 영역 */}
				<div className="h-20 flex items-center justify-between px-6 border-b border-slate-100">
					{isSidebarOpen ? (
						<div className="flex items-center gap-3">
							<div className="w-8 h-8 bg-linear-to-br from-sky-400 to-cyan-400 rounded-lg flex items-center justify-center shadow-md shadow-sky-200">
								<Play className="w-4 h-4 text-white fill-white ml-0.5" />
							</div>
							<span className="text-lg font-display font-bold text-slate-800 tracking-tight">
								Bot Admin
							</span>
						</div>
					) : (
						<div className="mx-auto w-8 h-8 bg-linear-to-br from-sky-400 to-cyan-400 rounded-lg flex items-center justify-center shadow-md shadow-sky-200">
							<Play className="w-4 h-4 text-white fill-white ml-0.5" />
						</div>
					)}
					{isSidebarOpen && (
						<button
							onClick={() => {
								setIsSidebarOpen(false);
							}}
							className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors"
							aria-label="사이드바 닫기"
						>
							<X size={18} />
						</button>
					)}
				</div>

				{!isSidebarOpen && (
					<div className="py-4 flex justify-center border-b border-slate-100">
						<button
							onClick={() => {
								setIsSidebarOpen(true);
							}}
							className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors"
							aria-label="사이드바 열기"
						>
							<Menu size={20} />
						</button>
					</div>
				)}

				{/* 네비게이션 */}
				<nav className="flex-1 py-6 px-3 overflow-y-auto scrollbar-hide animate-fade-in">
					{navGroups.map((group, groupIndex) => (
						<div
							key={group.title || groupIndex}
							className="mb-6 last:mb-0 animate-slide-in-left"
							style={{ animationDelay: `${String(groupIndex * 80)}ms` }}
						>
							{isSidebarOpen && group.title && (
								<div className="px-3 mb-2">
									<h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
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
													? "bg-sky-500 text-white shadow-md shadow-sky-200 scale-[1.02]"
													: "text-slate-500 hover:bg-slate-50 hover:text-slate-900",
											)
										}
										title={!isSidebarOpen ? item.label : undefined}
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
															: "text-slate-400 group-hover:text-slate-600",
													)}
												/>
												{isSidebarOpen && (
													<span className="ml-3 font-medium whitespace-nowrap">
														{item.label}
													</span>
												)}
												{isActive && (
													<div className="absolute left-0 top-1/2 -translate-y-1/2 w-1 h-8 bg-white/30 rounded-r-full" />
												)}
											</>
										)}
									</NavLink>
								))}
							</div>
						</div>
					))}
				</nav>

				{/* 유저 프로필 / 로그아웃 */}
				<div className="p-4 border-t border-slate-100">
					<button
						onClick={handleLogout}
						className={clsx(
							"flex items-center w-full p-3.5 rounded-xl hover:bg-rose-50 text-slate-500 hover:text-rose-600 transition-colors group",
							!isSidebarOpen && "justify-center",
						)}
						aria-label="로그아웃"
					>
						<LogOut
							size={20}
							className="group-hover:stroke-rose-600 transition-colors"
						/>
						{isSidebarOpen && (
							<span className="ml-3 font-medium">로그아웃</span>
						)}
					</button>
				</div>
			</aside>

			{/* 메인 콘텐츠 */}
			<main className="flex-1 flex flex-col min-w-0 overflow-hidden relative z-10">
				{/* 헤더 - Glass 효과 */}
				<header className="h-20 bg-white/60 backdrop-blur-md border-b border-slate-200/50 flex items-center justify-between px-8 sticky top-0 z-20">
					<div className="animate-fade-in-up">
						<h2 className="text-2xl font-display font-bold text-slate-800 tracking-tight">
							{activeRoute?.label || "대시보드"}
						</h2>
						<p className="text-xs text-slate-400 font-medium mt-0.5 tracking-wide">
							Unified Bot Management System
						</p>
					</div>

					<div className="flex items-center space-x-4">
						<div className="flex items-center space-x-3 px-1 py-1 bg-white border border-slate-200 rounded-full shadow-sm pr-4">
							<div className="w-8 h-8 rounded-full bg-gradient-to-tr from-sky-400 to-cyan-400 flex items-center justify-center text-white font-bold text-sm shadow-sm ring-2 ring-white">
								A
							</div>
							<div className="flex flex-col">
								<span className="text-sm font-display font-bold text-slate-700 leading-none">
									Administrator
								</span>
								<span className="text-[10px] text-sky-500 font-medium leading-none mt-1">
									Super User
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

import { queryClient } from "@/lib/queryClient";
