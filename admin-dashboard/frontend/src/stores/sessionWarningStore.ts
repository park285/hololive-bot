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
	setSessionPolicy: (policy: SessionPolicy) => void;
	setAbsoluteExpiresAt: (unixSeconds: number) => void;
	markSessionActivity: (nowMs: number) => void;
	openIdleWarning: () => void;
	closeIdleWarning: () => void;
	openAbsoluteWarning: () => void;
	closeAbsoluteWarning: () => void;
	resetSessionWarnings: () => void;
}

export const useSessionWarningStore = create<SessionWarningState>()((set) => ({
	absoluteExpiresAt: null,
	policy: null,
	lastActivityAtMs: Date.now(),
	idleWarningOpen: false,
	absoluteWarningOpen: false,

	setSessionPolicy: (policy) => {
		set({ policy });
	},
	setAbsoluteExpiresAt: (unixSeconds) => {
		set({ absoluteExpiresAt: unixSeconds });
	},
	markSessionActivity: (nowMs) => {
		set({ lastActivityAtMs: nowMs });
	},
	openIdleWarning: () => {
		set({ idleWarningOpen: true });
	},
	closeIdleWarning: () => {
		set({ idleWarningOpen: false });
	},
	openAbsoluteWarning: () => {
		set({ absoluteWarningOpen: true });
	},
	closeAbsoluteWarning: () => {
		set({ absoluteWarningOpen: false });
	},
	resetSessionWarnings: () => {
		set({
			absoluteExpiresAt: null,
			policy: null,
			lastActivityAtMs: Date.now(),
			idleWarningOpen: false,
			absoluteWarningOpen: false,
		});
	},
}));
