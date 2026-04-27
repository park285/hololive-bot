import Clock from "lucide-react/dist/esm/icons/clock";
import LogOut from "lucide-react/dist/esm/icons/log-out";
import RefreshCcw from "lucide-react/dist/esm/icons/refresh-ccw";
import { useEffect, useState } from "react";
import { authApi } from "@/api/core";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { clearClientSession } from "@/lib/sessionLifecycle";
import toast from "@/lib/toast-api";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export const SessionIdleWarningModal = () => {
	const {
		idleWarningOpen,
		lastActivityAtMs,
		policy,
		closeIdleWarning,
		markSessionActivity,
		setAbsoluteExpiresAt,
	} = useSessionWarningStore();

	const [remainingSeconds, setRemainingSeconds] = useState(0);
	const [isExtending, setIsExtending] = useState(false);
	const [isLoggingOut, setIsLoggingOut] = useState(false);

	useEffect(() => {
		if (!idleWarningOpen || !policy) return;

		const updateCountdown = () => {
			const now = Date.now();
			const idleTimeoutMs = lastActivityAtMs + policy.idle_timeout_ms;
			const remainingMs = idleTimeoutMs - now;
			setRemainingSeconds(Math.max(0, Math.ceil(remainingMs / 1000)));
		};

		updateCountdown();
		const interval = window.setInterval(updateCountdown, 1000);
		return () => {
			window.clearInterval(interval);
		};
	}, [idleWarningOpen, lastActivityAtMs, policy]);

	const handleExtend = async () => {
		if (isExtending) {
			return;
		}

		setIsExtending(true);
		try {
			const response = await authApi.heartbeat(false);

			if (response.idle_rejected || response.absolute_expired || response.error) {
				clearClientSession(true);
				toast.error(response.error ?? "세션을 연장하지 못했습니다.");
				return;
			}

			if (response.absolute_expires_at !== undefined) {
				setAbsoluteExpiresAt(response.absolute_expires_at);
			}

			markSessionActivity(Date.now());
			closeIdleWarning();
			toast.success("세션이 연장되었습니다.");
		} catch {
			toast.error("서버와 통신하지 못해 세션을 연장하지 못했습니다.");
		} finally {
			setIsExtending(false);
		}
	};

	const handleLogout = async () => {
		if (isLoggingOut) {
			return;
		}

		setIsLoggingOut(true);
		try {
			await authApi.logout();
		} finally {
			setIsLoggingOut(false);
			clearClientSession(true);
		}
	};

	return (
		<BaseModal
			isOpen={idleWarningOpen}
			onClose={() => {
				// 보안상 닫기, ESC, overlay 동작만으로 세션을 연장하지 않습니다.
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
						disabled={isExtending || isLoggingOut}
					>
						<LogOut className="mr-2 h-4 w-4" />
						{isLoggingOut ? "로그아웃 중…" : "로그아웃"}
					</Button>
					<Button
						variant="primary"
						className="flex-1 bg-amber-600 text-white hover:bg-amber-700 shadow-amber-200/50"
						onClick={() => {
							void handleExtend();
						}}
						disabled={isExtending || isLoggingOut}
					>
						<RefreshCcw className="mr-2 h-4 w-4" />
						{isExtending ? "연장 중…" : "세션 연장"}
					</Button>
				</div>
			</div>
		</BaseModal>
	);
};
