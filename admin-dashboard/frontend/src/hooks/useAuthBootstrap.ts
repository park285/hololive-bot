import { useEffect } from "react";
import { authApi } from "@/api/core";
import { applySessionStatus, clearClientSession } from "@/lib/sessionLifecycle";
import { useAuthStore } from "@/stores/authStore";

export function useAuthBootstrap() {
	const markAuthPending = useAuthStore((state) => state.markAuthPending);

	useEffect(() => {
		const lifecycle = { cancelled: false };
		const controller = new AbortController();

		markAuthPending();

		void (async () => {
			try {
				const session = await authApi.getSession(controller.signal);
				if (lifecycle.cancelled) {
					return;
				}

				applySessionStatus(session);
			} catch (error) {
				if (lifecycle.cancelled || (error instanceof Error && error.name === "AbortError")) {
					return;
				}
				clearClientSession();
			}
		})();

		return () => {
			lifecycle.cancelled = true;
			controller.abort();
		};
	}, [markAuthPending]);
}
