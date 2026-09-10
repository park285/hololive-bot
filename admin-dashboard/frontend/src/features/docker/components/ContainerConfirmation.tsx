import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle.mjs";
import { ConfirmModal } from "@/components/ConfirmModal";
import { actionLabels, type ContainerIntent } from "@/features/docker/hooks/useContainers";

interface Props { intent: ContainerIntent; canConfirm: boolean; onClose: () => void; onConfirm: () => void }

export function ContainerConfirmation({ intent, canConfirm, onClose, onConfirm }: Props) {
	const label = actionLabels[intent.action];
	return <ConfirmModal isOpen title={`컨테이너 ${label}`} message="" confirmText={`${label} 실행`} confirmColor={intent.action === "stop" ? "danger" : "primary"} onClose={onClose} onConfirm={onConfirm} canConfirm={canConfirm}>
		<div className="space-y-3">
			<p className="flex items-center gap-3 text-muted-foreground"><AlertTriangle className="shrink-0 text-amber-600" size={24} aria-hidden="true" /><strong className="break-all text-foreground">{intent.name}</strong> {label} 작업을 실행하시겠습니까?</p>
			{intent.action !== "start" && <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700">서비스가 잠시 중단될 수 있습니다.</p>}
			{!canConfirm && <p role="status">최신 컨테이너 상태와 작업 권한을 확인해야 실행할 수 있습니다.</p>}
		</div>
	</ConfirmModal>;
}
