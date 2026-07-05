import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle.mjs";
import {
	type ConfirmModalState,
	type ContainerAction,
	getActionLabel,
} from "@/components/settings/DockerContainerActions";
import { ConfirmModal } from "@/components/ConfirmModal";

interface DockerContainerConfirmModalProps {
	confirmModal: ConfirmModalState;
	onClose: () => void;
	onConfirm: () => void;
}

export const DockerContainerConfirmModal = ({
	confirmModal,
	onClose,
	onConfirm,
}: DockerContainerConfirmModalProps) => {
	const label = getActionLabel(confirmModal.action);
	const confirmColor = resolveConfirmColor(confirmModal.action);

	return (
		<ConfirmModal
			isOpen={confirmModal.isOpen}
			onClose={onClose}
			onConfirm={onConfirm}
			title={`컨테이너 ${label}`}
			message={""}
			confirmText={`${label} 실행`}
			confirmColor={confirmColor}
		>
			<div className="space-y-3">
				<div className="flex items-center gap-3 mb-2">
					<div className="bg-amber-100 p-2.5 rounded-full shrink-0">
						<AlertTriangle className="text-amber-600" size={24} />
					</div>
					<p className="text-muted-foreground">
						'
						<span className="font-bold text-foreground">
							{confirmModal.containerName}
						</span>
						'을(를) {label}하시겠습니까?
					</p>
				</div>

				{confirmModal.action !== "start" && (
					<p className="text-xs text-amber-600 font-medium bg-amber-50 px-3 py-2 rounded-lg">
						주의: 서비스가 잠시 중단될 수 있습니다.
					</p>
				)}
			</div>
		</ConfirmModal>
	);
};

function resolveConfirmColor(
	action: ContainerAction | null,
): "primary" | "danger" {
	return action === "stop" ? "danger" : "primary";
}
