import { useEffect } from "react";
import { authApi } from "@/api/core";
import { useAuthStore } from "@/stores/authStore";

export function useAuthBootstrap() {
	const markAuthPending = useAuthStore((state) => state.markAuthPending);
	const setAuthenticated = useAuthStore((state) => state.setAuthenticated);

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
			} catch {
				if (lifecycle.cancelled) {
					return;
				}
				setAuthenticated(false);
			}
		})();

		return () => {
			lifecycle.cancelled = true;
		};
	}, [markAuthPending, setAuthenticated]);
}
