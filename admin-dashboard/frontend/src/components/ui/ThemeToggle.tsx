import Monitor from "lucide-react/dist/esm/icons/monitor.mjs";
import Moon from "lucide-react/dist/esm/icons/moon.mjs";
import Sun from "lucide-react/dist/esm/icons/sun.mjs";
import { Button } from "@/components/ui/Button";
import type { ThemePreference } from "@/lib/theme";
import { useThemeStore } from "@/stores/themeStore";

const NEXT_PREFERENCE: Record<ThemePreference, ThemePreference> = {
	light: "dark",
	dark: "system",
	system: "light",
};

const PREFERENCE_LABEL: Record<ThemePreference, string> = {
	light: "라이트",
	dark: "다크",
	system: "시스템",
};

const PREFERENCE_ICON: Record<ThemePreference, typeof Sun> = {
	light: Sun,
	dark: Moon,
	system: Monitor,
};

export const ThemeToggle = () => {
	const preference = useThemeStore((state) => state.preference);
	const setPreference = useThemeStore((state) => state.setPreference);

	const next = NEXT_PREFERENCE[preference];
	const Icon = PREFERENCE_ICON[preference];

	return (
		<Button
			type="button"
			variant="ghost"
			size="icon"
			onClick={() => {
				setPreference(next);
			}}
			aria-label={`테마 전환: 현재 ${PREFERENCE_LABEL[preference]}, 클릭 시 ${PREFERENCE_LABEL[next]}`}
			title={`테마: ${PREFERENCE_LABEL[preference]}`}
		>
			<Icon aria-hidden="true" />
		</Button>
	);
};
