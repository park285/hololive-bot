import { clearCSRFToken } from "@/api/client";
import { authApi, type SessionStatusResponse } from "@/api/core";
import { broadcastSessionLogout } from "@/hooks/useActivityDetection";
import { queryClient } from "@/lib/queryClient";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export const applySessionStatus = (
	session: SessionStatusResponse,
	nowMs: number = Date.now(),
): void => {
	const authStore = useAuthStore.getState();
	const sessionWarningStore = useSessionWarningStore.getState();

	if (!session.authenticated) {
		authStore.setAuthenticated(false);
		sessionWarningStore.resetSessionWarnings();
		queryClient.clear();
		return;
	}

	authStore.setAuthenticated(true);
	sessionWarningStore.setSessionPolicy(session.session_policy);
	sessionWarningStore.setAbsoluteExpiresAt(session.absolute_expires_at);
	sessionWarningStore.markSessionActivity(nowMs);
};

export const clearClientSession = (broadcast = false): void => {
	if (broadcast) {
		broadcastSessionLogout();
	}

	clearCSRFToken();
	useAuthStore.getState().logout();
	useSessionWarningStore.getState().resetSessionWarnings();
	queryClient.clear();
};

export const logoutEverywhere = async (): Promise<void> => {
	try {
		await authApi.logout();
	} finally {
		clearClientSession(true);
	}
};

/** 현재 공유 쿠키로 세션을 조회하여 각 탭의 CSRF와 표시 상태를 동기화합니다. */
export const refreshClientSession = async (signal?: AbortSignal): Promise<void> => {
	const session = await authApi.getSession(signal);
	applySessionStatus(session);
};
