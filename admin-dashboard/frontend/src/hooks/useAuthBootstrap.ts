import { useEffect } from "react";
import { authApi } from "@/api/core";
import { useAuthStore } from "@/stores/authStore";
import { useSessionWarningStore } from "@/stores/sessionWarningStore";

export function useAuthBootstrap() {
	const markAuthPending = useAuthStore((state) => state.markAuthPending);
	const setAuthenticated = useAuthStore((state) => state.setAuthenticated);
	const setSessionPolicy = useSessionWarningStore((state) => state.setSessionPolicy);
	const setAbsoluteExpiresAt = useSessionWarningStore((state) => state.setAbsoluteExpiresAt);
	const markSessionActivity = useSessionWarningStore((state) => state.markSessionActivity);
	const resetSessionWarnings = useSessionWarningStore((state) => state.resetSessionWarnings);

	useEffect(() => {
		const lifecycle = { cancelled: false };

		markAuthPending();

		void (async () => {
			try {
				const session = await authApi.getSession();
				if (lifecycle.cancelled) {
					return;
				}
				
				setAuthenticated(session.authenticated);
				
				if (session.authenticated) {
					setSessionPolicy(session.session_policy);
					setAbsoluteExpiresAt(session.absolute_expires_at);
					markSessionActivity(Date.now());
				} else {
					resetSessionWarnings();
				}
			} catch {
				if (lifecycle.cancelled) {
					return;
				}
				setAuthenticated(false);
				resetSessionWarnings();
			}
		})();

		return () => {
			lifecycle.cancelled = true;
		};
	}, [
		markAuthPending, 
		setAuthenticated, 
		setSessionPolicy, 
		setAbsoluteExpiresAt, 
		markSessionActivity, 
		resetSessionWarnings
	]);
}
