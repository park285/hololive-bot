import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle.mjs";
import ShieldAlert from "lucide-react/dist/esm/icons/shield-alert.mjs";
import { useEffect, useState } from "react";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export const SessionAbsoluteWarningModal = () => {
	const {
		absoluteWarningOpen,
		absoluteExpiresAt,
		dismissAbsoluteWarning,
	} = useSessionWarningStore();

	const [remainingSeconds, setRemainingSeconds] = useState(0);

	useEffect(() => {
		if (!absoluteWarningOpen || !absoluteExpiresAt) return;

		const updateCountdown = () => {
			const now = Date.now();
			const expiryMs = absoluteExpiresAt * 1000;
			const remainingMs = expiryMs - now;
			setRemainingSeconds(Math.max(0, Math.floor(remainingMs / 1000)));
		};

		updateCountdown();
		const interval = setInterval(updateCountdown, 1000);
		return () => {
			clearInterval(interval);
		};
	}, [absoluteWarningOpen, absoluteExpiresAt]);

	return (
		<BaseModal
			isOpen={absoluteWarningOpen}
			onClose={dismissAbsoluteWarning}
			title={
				<div className="flex items-center gap-2 text-red-600">
					<ShieldAlert className="h-5 w-5" />
					<span>세션이 곧 만료됩니다</span>
				</div>
			}
		>
			<div className="space-y-4">
				<div className="rounded-lg bg-red-50 p-3 text-sm text-red-800 border border-red-100 flex gap-3">
					<AlertTriangle className="h-5 w-5 shrink-0" />
					<p>
						보안 정책에 따라 최대 세션 유지 시간이 곧 만료됩니다.
						진행 중인 모든 작업을 저장하고 다시 로그인할 준비를 해주세요.
					</p>
				</div>

				<p className="text-slate-600 text-sm">
					절대 만료 시간은 보안을 위해 연장할 수 없습니다. 만료 시 자동으로 로그아웃 페이지로 이동합니다.
				</p>

				<div className="flex items-center justify-center rounded-xl bg-slate-50 p-6 border border-slate-100">
					<div className="text-center">
						<div className="text-sm font-medium text-slate-500">
							강제 로그아웃까지 남은 시간
						</div>
						<div className="mt-1 font-mono text-4xl font-bold text-slate-800">
							{Math.floor(remainingSeconds / 60)}:
							{String(remainingSeconds % 60).padStart(2, "0")}
						</div>
					</div>
				</div>

				<div className="pt-2">
					<Button
						variant="secondary"
						fullWidth
						className="bg-slate-800 text-white hover:bg-slate-900"
						onClick={dismissAbsoluteWarning}
					>
						확인했습니다
					</Button>
				</div>
			</div>
		</BaseModal>
	);
};
