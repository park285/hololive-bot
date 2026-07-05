import { applyTheme, readStoredPreference, resolveTheme } from "@/lib/theme";

const systemPrefersDark =
	typeof window !== "undefined" &&
	typeof window.matchMedia === "function" &&
	window.matchMedia("(prefers-color-scheme: dark)").matches;

applyTheme(resolveTheme(readStoredPreference(), systemPrefersDark));
