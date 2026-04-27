import { create } from "zustand";

export type SessionPolicy = {
	heartbeat_interval_ms: number;
	idle_timeout_ms: number;
	idle_warning_timeout_ms: number;
	idle_session_ttl_ms: number;
	absolute_warning_window_ms: number;
};

interface SessionWarningState {
	absoluteExpiresAt: number | null;
	policy: SessionPolicy | null;
	lastActivityAtMs: number;
	idleWarningOpen: boolean;
	absoluteWarningOpen: boolean;
	absoluteWarningDismissedForExpiresAt: number | null;
	setSessionPolicy: (policy: SessionPolicy | null) => void;
	setAbsoluteExpiresAt: (unixSeconds: number | null) => void;
	markSessionActivity: (nowMs?: number) => void;
	openIdleWarning: () => void;
	closeIdleWarning: () => void;
	openAbsoluteWarning: () => void;
	closeAbsoluteWarning: () => void;
	dismissAbsoluteWarning: () => void;
	resetSessionWarnings: () => void;
}

export const useSessionWarningStore = create<SessionWarningState>()((set) => ({
	absoluteExpiresAt: null,
	policy: null,
	lastActivityAtMs: Date.now(),
	idleWarningOpen: false,
	absoluteWarningOpen: false,
	absoluteWarningDismissedForExpiresAt: null,

	setSessionPolicy: (policy) => {
		set((state) => (state.policy === policy ? state : { policy }));
	},
	setAbsoluteExpiresAt: (unixSeconds) => {
		set((state) => {
			if (state.absoluteExpiresAt === unixSeconds) {
				return state;
			}

			return {
				absoluteExpiresAt: unixSeconds,
				absoluteWarningDismissedForExpiresAt: null,
			};
		});
	},
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
	dismissAbsoluteWarning: () => {
		set((state) => {
			if (state.absoluteExpiresAt === null) {
				return state;
			}

			return {
				absoluteWarningOpen: false,
				absoluteWarningDismissedForExpiresAt: state.absoluteExpiresAt,
			};
		});
	},
	resetSessionWarnings: () => {
		set({
			absoluteExpiresAt: null,
			policy: null,
			lastActivityAtMs: Date.now(),
			idleWarningOpen: false,
			absoluteWarningOpen: false,
			absoluteWarningDismissedForExpiresAt: null,
		});
	},
}));
