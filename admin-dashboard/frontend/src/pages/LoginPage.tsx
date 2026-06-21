import { useMutation } from "@tanstack/react-query";
import axios from "axios";
import ArrowRight from "lucide-react/dist/esm/icons/arrow-right.mjs";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import Lock from "lucide-react/dist/esm/icons/lock.mjs";
import Play from "lucide-react/dist/esm/icons/play.mjs";
import User from "lucide-react/dist/esm/icons/user.mjs";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { authApi } from "@/api/core";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { applySessionStatus } from "@/lib/sessionLifecycle";
import { queryClient } from "@/lib/queryClient";

import { getErrorMessageFromUnknown } from "@/lib/typeUtils";

const LoginPage = () => {
	const navigate = useNavigate();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [error, setError] = useState("");

	const loginMutation = useMutation({
		mutationFn: async () => {
			const normalizedUsername = username.trim();
			await authApi.login(normalizedUsername, password);
			return authApi.getSession();
		},
		onSuccess: (session) => {
			if (!session.authenticated) {
				setError("로그인 세션을 확인하지 못했습니다. 다시 로그인해주세요.");
				return;
			}

			queryClient.clear();
			applySessionStatus(session);
			setPassword("");
			void navigate("/dashboard/stats", { replace: true });
		},
		onError: (err: unknown) => {
			if (axios.isAxiosError(err)) {
				if (err.response?.status === 429) {
					setError(
						"너무 많은 로그인 시도가 감지되었습니다. 15분 후 다시 시도해주세요.",
					);
					return;
				}
				if (err.response?.status && err.response.status >= 500) {
					setError("서버 오류가 발생했습니다. 잠시 후 다시 시도해주세요.");
					return;
				}
			}
			setError(getErrorMessageFromUnknown(err));
		},
	});

	const handleSubmit = (e: React.SyntheticEvent<HTMLFormElement>) => {
		e.preventDefault();
		setError("");

		if (!username.trim() || !password) {
			setError("아이디와 비밀번호를 입력해주세요");
			return;
		}

		void loginMutation.mutateAsync().catch(() => undefined);
	};

	return (
		<div className="min-h-screen w-full flex items-center justify-center relative overflow-hidden bg-slate-50 font-body selection:bg-sky-200">
			<div className="absolute inset-0 bg-white z-0">
				<div className="absolute top-0 left-0 right-0 h-[500px] bg-linear-to-b from-sky-100/50 to-transparent"></div>
				<div className="absolute -top-24 right-0 w-[500px] h-[500px] bg-sky-200/30 rounded-full blur-[100px] animate-pulse"></div>
				<div className="absolute top-1/2 -left-24 w-[400px] h-[400px] bg-cyan-100/40 rounded-full blur-[80px]"></div>
				<div
					className="absolute inset-0 opacity-[0.015]"
					style={{
						backgroundImage:
							"url(\"data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
					}}
				/>
			</div>

			<div className="w-full max-w-[400px] z-10 px-6">
				<div className="relative">
					<div className="text-center mb-10 animate-fade-in-up">
						<div className="inline-flex items-center justify-center w-16 h-16 bg-linear-to-tr from-sky-400 to-cyan-400 rounded-2xl shadow-lg shadow-sky-200/60 mb-6 transform rotate-3 hover:rotate-6 transition-transform duration-300">
							<Play
								className="w-8 h-8 text-white fill-white ml-1"
								aria-hidden="true"
							/>
						</div>

						<h1 className="text-2xl font-display font-bold text-slate-800 tracking-tight">
							Hololive Bot <span className="text-sky-500">콘솔</span>
						</h1>
					</div>

					<form
						onSubmit={handleSubmit}
						className="space-y-5 animate-fade-in-up stagger-2"
					>
						<div className="space-y-4">
							<div className="group relative">
								<Label htmlFor="username" className="sr-only">
									아이디
								</Label>
								<div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none text-slate-400 group-focus-within:text-sky-500 transition-colors z-10">
									<User size={18} aria-hidden="true" />
								</div>
								<Input
									id="username"
									type="text"
									name="username"
									autoComplete="username"
									value={username}
									onChange={(e) => {
										setUsername(e.target.value);
									}}
									className="pl-11 pr-4 py-6 bg-white border-slate-200 rounded-xl text-slate-800 placeholder-slate-400 focus-visible:border-sky-400 focus-visible:ring-4 focus-visible:ring-sky-100 transition-colors shadow-sm font-medium"
									placeholder="아이디"
								/>
							</div>

							<div className="group relative">
								<Label htmlFor="password" className="sr-only">
									비밀번호
								</Label>
								<div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none text-slate-400 group-focus-within:text-sky-500 transition-colors z-10">
									<Lock size={18} aria-hidden="true" />
								</div>
								<Input
									id="password"
									type="password"
									name="password"
									autoComplete="current-password"
									value={password}
									onChange={(e) => {
										setPassword(e.target.value);
									}}
									className="pl-11 pr-4 py-6 bg-white border-slate-200 rounded-xl text-slate-800 placeholder-slate-400 focus-visible:border-sky-400 focus-visible:ring-4 focus-visible:ring-sky-100 transition-colors shadow-sm font-medium"
									placeholder="비밀번호"
								/>
							</div>
						</div>

						{error && (
							<div
								role="alert"
								aria-live="polite"
								className="text-rose-500 text-sm bg-rose-50 px-4 py-3 rounded-xl border border-rose-100 flex items-center font-medium"
							>
								<div className="w-1.5 h-1.5 rounded-full bg-rose-500 mr-2.5" />
								{error}
							</div>
						)}

						<Button
							type="submit"
							disabled={loginMutation.isPending}
							className="w-full relative overflow-hidden flex justify-center items-center py-6 px-4 bg-linear-to-r from-slate-800 to-slate-900 rounded-xl text-sm font-display font-bold text-white hover:from-slate-700 hover:to-slate-800 focus-visible:ring-4 focus-visible:ring-slate-200 disabled:opacity-70 disabled:cursor-not-allowed transition-all duration-200 shadow-xl shadow-slate-300/60 hover:shadow-2xl hover:shadow-sky-300/40 hover:-translate-y-0.5 group"
						>
							<div className="relative z-10 flex items-center justify-center">
								{loginMutation.isPending ? (
									<>
										<div className="animate-spin mr-2">
											<Loader2 className="h-5 w-5" aria-hidden="true" />
										</div>
										연결 중…
									</>
								) : (
									<>
										로그인
										<span className="inline-flex ml-2">
											<ArrowRight
												className="h-4 w-4 transition-transform duration-300 group-hover:translate-x-1"
												aria-hidden="true"
											/>
										</span>
									</>
								)}
							</div>
						</Button>
					</form>

					<div className="mt-12 text-center space-y-2 animate-fade-in stagger-4">
						<div className="flex justify-center space-x-2">
							<div className="w-1.5 h-1.5 rounded-full bg-linear-to-br from-sky-400 to-cyan-400"></div>
							<div className="w-1.5 h-1.5 rounded-full bg-linear-to-br from-cyan-400 to-teal-400"></div>
							<div className="w-1.5 h-1.5 rounded-full bg-linear-to-br from-teal-400 to-emerald-400"></div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
};

export default LoginPage;
