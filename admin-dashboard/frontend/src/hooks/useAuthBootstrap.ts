import { useEffect } from "react";
import { authApi } from "@/api/core";
import { applySessionStatus, clearClientSession } from "@/lib/sessionLifecycle";
import { useAuthStore } from "@/stores/authStore";

export function useAuthBootstrap() {
	const markAuthPending = useAuthStore((state) => state.markAuthPending);

	useEffect(() => {
		const lifecycle = { cancelled: false };

		markAuthPending();

		void (async () => {
			try {
				const session = await authApi.getSession();
				if (lifecycle.cancelled) {
					return;
				}

				applySessionStatus(session);
			} catch {
				if (lifecycle.cancelled) {
					return;
				}
				clearClientSession();
			}
		})();

		return () => {
			lifecycle.cancelled = true;
		};
	}, [markAuthPending]);
}
