import { DockerContainersSection } from "@/features/settings/components/DockerContainersSection";
import { SettingsFormSection } from "@/features/settings/components/SettingsFormSection";

export const SettingsPage = () => {
	return (
		<div className="max-w-4xl mx-auto space-y-6">
			<SettingsFormSection />
			<DockerContainersSection />
		</div>
	);
};
