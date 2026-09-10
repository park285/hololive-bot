import { lazy, Suspense, useState, type ReactNode } from "react";
import { useStore } from "zustand";
import { warningState } from "@/session/warnings";
import { useActivityDetection } from "@/session/useActivity";
import { useHeartbeat } from "@/session/useHeartbeat";
import { useSessionWarnings } from "@/session/useWarnings";
import type { SessionPolicyResponse } from "@/api/generated/data-contracts";

const SessionWarningDialogs = lazy(() => import("@/session/SessionWarningDialogs"));

/** AuthenticatedSession은 인증 세대별 UI·timer를 소유하여 새 로그인에 이전 상태가 남지 않게 합니다. */
export function AuthenticatedSession({ policy, children }: { policy: SessionPolicyResponse; children: ReactNode }) {
	const isIdle = useActivityDetection({ enabled: true, idleTimeoutMs: policy.idle_timeout_ms });
	useHeartbeat(isIdle);
	useSessionWarnings(isIdle);
	return <>{children}<SessionWarningPresentation /></>;
}

/** SessionWarningPresentation은 첫 경고를 표시할 때 UI를 가져오고 인증 수명 안에서 유지합니다. */
export function SessionWarningPresentation() {
	const warningOpen = useStore(warningState, state => state.idleWarningOpen || state.absoluteWarningOpen);
	const [dialogsRequested, setDialogsRequested] = useState(false);
	// 첫 알림 뒤에는 component를 유지하여 연장·로그아웃 응답 전에 로컬 잠금이 사라지지 않게 합니다.
	if (warningOpen && !dialogsRequested) setDialogsRequested(true);
	return dialogsRequested && <Suspense fallback={<p role="alert" className="fixed bottom-4 left-4 right-4 z-50 rounded-lg border border-border bg-card p-4 text-foreground shadow-lg">세션 만료가 임박했습니다. 알림을 준비하고 있습니다.</p>}><SessionWarningDialogs /></Suspense>;
}
