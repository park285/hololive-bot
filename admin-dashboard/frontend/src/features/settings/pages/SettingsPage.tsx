import { ContainerList } from "@/features/docker/components/ContainerList";
import { SettingsForm } from "@/features/settings/components/SettingsForm";

export const SettingsPage = () => (
	<div className="max-w-4xl mx-auto space-y-6">
		<SettingsForm />
		<ContainerList />
	</div>
);
