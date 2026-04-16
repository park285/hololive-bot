import Clock from "lucide-react/dist/esm/icons/clock";
import LogOut from "lucide-react/dist/esm/icons/log-out";
import RefreshCcw from "lucide-react/dist/esm/icons/refresh-ccw";
import { useEffect, useState } from "react";
import { authApi } from "@/api/core";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export const SessionIdleWarningModal = () => {
	const logout = useAuthStore((state) => state.logout);
	const {
		idleWarningOpen,
		lastActivityAtMs,
		policy,
		closeIdleWarning,
		markSessionActivity,
	} = useSessionWarningStore();

	const [remainingSeconds, setRemainingSeconds] = useState(0);

	useEffect(() => {
		if (!idleWarningOpen || !policy) return;

		const updateCountdown = () => {
			const now = Date.now();
			const idleTimeoutMs = lastActivityAtMs + policy.idle_timeout_ms;
			const remainingMs = idleTimeoutMs - now;
			setRemainingSeconds(Math.max(0, Math.floor(remainingMs / 1000)));
		};

		updateCountdown();
		const interval = setInterval(updateCountdown, 1000);
		return () => {
			clearInterval(interval);
		};
	}, [idleWarningOpen, lastActivityAtMs, policy]);

	const handleExtend = async () => {
		try {
			markSessionActivity(Date.now());
			closeIdleWarning();
			await authApi.heartbeat(false);
		} catch (error) {
			console.error("Failed to extend session:", error);
		}
	};

	const handleLogout = async () => {
		try {
			await authApi.logout();
		} finally {
			logout();
			closeIdleWarning();
		}
	};

	return (
		<BaseModal
			isOpen={idleWarningOpen}
			onClose={() => {
				void handleExtend();
			}}
			title={
				<div className="flex items-center gap-2 text-amber-600">
					<Clock className="h-5 w-5" />
					<span>곧 자동 로그아웃됩니다</span>
				</div>
			}
		>
			<div className="space-y-4">
				<p className="text-slate-600">
					장시간 활동이 없어 잠시 후 보안을 위해 자동으로 로그아웃됩니다.
					계속해서 서비스를 이용하시려면 세션을 연장해주세요.
				</p>

				<div className="flex items-center justify-center rounded-xl bg-amber-50 p-6">
					<div className="text-center">
						<div className="text-sm font-medium text-amber-800 opacity-70">
							자동 로그아웃까지 남은 시간
						</div>
						<div className="mt-1 font-mono text-4xl font-bold text-amber-600">
							{Math.floor(remainingSeconds / 60)}:
							{String(remainingSeconds % 60).padStart(2, "0")}
						</div>
					</div>
				</div>

				<div className="flex gap-3 pt-2">
					<Button
						variant="outline"
						className="flex-1 border-slate-200 text-slate-600 hover:bg-slate-50"
						onClick={() => {
							void handleLogout();
						}}
					>
						<LogOut className="mr-2 h-4 w-4" />
						로그아웃
					</Button>
					<Button
						variant="primary"
						className="flex-1 bg-amber-600 text-white hover:bg-amber-700 shadow-amber-200/50"
						onClick={() => {
							void handleExtend();
						}}
					>
						<RefreshCcw className="mr-2 h-4 w-4" />
						세션 연장
					</Button>
				</div>
			</div>
		</BaseModal>
	);
};
