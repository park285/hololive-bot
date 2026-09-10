import { createStore } from "zustand/vanilla";

interface WarningPresentation {
	lastActivityAtMs: number;
	idleWarningOpen: boolean;
	absoluteWarningOpen: boolean;
	absoluteWarningDismissedForExpiresAt: number | null;
	markSessionActivity: (nowMs?: number) => void;
	openIdleWarning: () => void;
	closeIdleWarning: () => void;
	openAbsoluteWarning: () => void;
	closeAbsoluteWarning: () => void;
	dismissAbsoluteWarning: (expiresAt: number | null) => void;
	resetSessionWarnings: () => void;
}

/** warningState는 알림 표시와 사용자 활동만 소유하며 인증·정책은 SessionState에서 읽습니다. */
export const warningState = createStore<WarningPresentation>()((set) => ({
	lastActivityAtMs: Date.now(),
	idleWarningOpen: false,
	absoluteWarningOpen: false,
	absoluteWarningDismissedForExpiresAt: null,

	markSessionActivity: (nowMs = Date.now()) => {
		set({
			lastActivityAtMs: nowMs,
			idleWarningOpen: false,
		});
	},
	openIdleWarning: () => {
		set((state) => (state.idleWarningOpen ? state : { idleWarningOpen: true }));
	},
	closeIdleWarning: () => {
		set((state) => (state.idleWarningOpen ? { idleWarningOpen: false } : state));
	},
	openAbsoluteWarning: () => {
		set((state) =>
			state.absoluteWarningOpen ? state : { absoluteWarningOpen: true },
		);
	},
	closeAbsoluteWarning: () => {
		set((state) =>
			state.absoluteWarningOpen ? { absoluteWarningOpen: false } : state,
		);
	},
	dismissAbsoluteWarning: (expiresAt) => {
		set((state) => {
			if (expiresAt === null) {
				return state;
			}

			return {
				absoluteWarningOpen: false,
				absoluteWarningDismissedForExpiresAt: expiresAt,
			};
		});
	},
	resetSessionWarnings: () => {
		set({
							lastActivityAtMs: Date.now(),
			idleWarningOpen: false,
			absoluteWarningOpen: false,
			absoluteWarningDismissedForExpiresAt: null,
		});
	},
}));
