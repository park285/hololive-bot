export type ThemePreference = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "theme";

export const resolveTheme = (
	preference: ThemePreference,
	systemPrefersDark: boolean,
): ResolvedTheme => {
	if (preference === "system") {
		return systemPrefersDark ? "dark" : "light";
	}
	return preference;
};

export const applyTheme = (resolved: ResolvedTheme): void => {
	document.documentElement.classList.toggle("dark", resolved === "dark");
};

export const readStoredPreference = (): ThemePreference => {
	try {
		const stored = globalThis.localStorage.getItem(STORAGE_KEY);
		if (stored === "light" || stored === "dark" || stored === "system") {
			return stored;
		}
	} catch {
		return "system";
	}
	return "system";
};

export const storePreference = (preference: ThemePreference): void => {
	try {
		globalThis.localStorage.setItem(STORAGE_KEY, preference);
	} catch {
		return;
	}
};
