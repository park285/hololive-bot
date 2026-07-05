import { create } from "zustand";
import {
	applyTheme,
	readStoredPreference,
	resolveTheme,
	storePreference,
	type ResolvedTheme,
	type ThemePreference,
} from "@/lib/theme";

interface ThemeState {
	preference: ThemePreference;
	resolved: ResolvedTheme;
	setPreference: (preference: ThemePreference) => void;
}

const PREFERS_DARK_QUERY = "(prefers-color-scheme: dark)";

const systemPrefersDark = (): boolean =>
	typeof window !== "undefined" &&
	typeof window.matchMedia === "function" &&
	window.matchMedia(PREFERS_DARK_QUERY).matches;

const initialPreference = readStoredPreference();

export const useThemeStore = create<ThemeState>()((set) => ({
	preference: initialPreference,
	resolved: resolveTheme(initialPreference, systemPrefersDark()),
	setPreference: (preference) => {
		const resolved = resolveTheme(preference, systemPrefersDark());
		applyTheme(resolved);
		storePreference(preference);
		set({ preference, resolved });
	},
}));

if (typeof window !== "undefined" && typeof window.matchMedia === "function") {
	window
		.matchMedia(PREFERS_DARK_QUERY)
		.addEventListener("change", (event) => {
			if (useThemeStore.getState().preference !== "system") {
				return;
			}
			const resolved: ResolvedTheme = event.matches ? "dark" : "light";
			applyTheme(resolved);
			useThemeStore.setState({ resolved });
		});
}
