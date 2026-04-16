import { create } from "zustand";
import { useSessionWarningStore } from "./sessionWarningStore";

interface AuthState {
	isAuthenticated: boolean;
	isAuthResolved: boolean;
	setAuthenticated: (value: boolean) => void;
	markAuthPending: () => void;
	logout: () => void;
}

export const useAuthStore = create<AuthState>()((set) => ({
	isAuthenticated: false,
	isAuthResolved: false,
	setAuthenticated: (value) => {
		set({ isAuthenticated: value, isAuthResolved: true });
	},
	markAuthPending: () => {
		set({ isAuthResolved: false });
	},
	logout: () => {
		set({ isAuthenticated: false, isAuthResolved: true });
		useSessionWarningStore.getState().resetSessionWarnings();
	},
}));
