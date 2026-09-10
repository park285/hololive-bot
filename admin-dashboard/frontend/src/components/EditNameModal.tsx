import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle.mjs";
import Save from "lucide-react/dist/esm/icons/save.mjs";
import { type ReactNode, type SyntheticEvent, useRef, useState } from "react";
import { useDraft } from "@/editors/useDraft";
import { DraftConflict } from "@/editors/DraftConflict";
import { RequestBlockedError } from "@/api/errors";
import { operations } from "@/app/bootstrap";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

interface EditNameModalProps {
	isOpen: boolean;
	onClose: () => void;
	onSave: (newName: string) => Promise<void>;
	canSubmit: boolean;
	pending: boolean;
	readState: ReactNode;
	type: "room" | "user" | "member";
	id: string;
	currentName: string | undefined;
}

export default function EditNameModal({
	isOpen,
	onClose,
	onSave,
	canSubmit,
	pending,
	readState,
	type,
	id,
	currentName,
}: EditNameModalProps) {
	const draft = useDraft(currentName);
	const name = draft.value;
	const [error, setError] = useState("");
	const nameInputRef = useRef<HTMLInputElement>(null);

	const isDirty = draft.dirty;
	const blocked = !canSubmit || pending || draft.conflict;

	const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (blocked) return;

		if (!name.trim()) {
			setError("이름을 입력해주세요.");
			queueMicrotask(() => {
				nameInputRef.current?.focus();
			});
			return;
		}

		void onSave(name.trim()).catch((cause: unknown) => {
			setError(operations.failure(cause)?.message ?? (cause instanceof RequestBlockedError ? cause.message : "작업 결과를 확인하지 못했습니다. 현재 상태를 다시 조회해 주세요."));
		});
	};

	const getTitle = () => {
		switch (type) {
			case "room":
				return "방 이름 수정";
			case "user":
				return "사용자 이름 수정";
			case "member":
				return "멤버 이름 수정";
			default:
				return "이름 수정";
		}
	};

	const showIdWarning = type !== "member" && /^\d+$/.test(id);

	return (
		<BaseModal
			isOpen={isOpen}
			onClose={onClose}
			title={getTitle()}
			showHeaderBorder
		>
			<form onSubmit={handleSubmit} className="space-y-4" noValidate>
				{readState}
				{draft.conflict && <DraftConflict latest={draft.latest} useLatest={draft.useLatest} keepDraft={draft.keepDraft} />}
				<div className="mb-4 rounded-lg border border-border-subtle bg-muted p-3">
					<div className="mb-1 text-xs font-medium text-muted-foreground">
						ID (변경 불가)
					</div>
					<div className="text-sm font-mono text-foreground">{id}</div>
				</div>

				{showIdWarning && (
					<div className="mb-4 flex items-start gap-2 rounded-lg border border-amber-100 bg-amber-50 p-3">
						<AlertTriangle
							size={16}
							className="mt-0.5 shrink-0 text-amber-500"
							aria-hidden="true"
						/>
						<div className="text-xs leading-snug text-amber-700">
							현재 ID가 사용 중입니다. 이름을 설정하면 ID 대신 이름이
							표시됩니다.
						</div>
					</div>
				)}

				<div className="space-y-2">
					<Label htmlFor="edit-name-input">새로운 이름</Label>
					<Input
						ref={nameInputRef}
						id="edit-name-input"
						name="name"
						autoComplete="off"
						value={name}
						onChange={(event) => {
							draft.setValue(event.target.value);
							setError("");
						}}
						placeholder="이름을 입력하세요"
						hasError={!!error}
						aria-invalid={!!error}
						aria-describedby={error ? "edit-name-error" : undefined}
					/>
					{error && (
						<p
							id="edit-name-error"
							role="status"
							aria-live="polite"
							className="text-[0.8rem] font-medium text-destructive"
						>
							{error}
						</p>
					)}
				</div>

				<div className="mt-6 flex justify-end gap-3 pt-2">
					<Button type="button" variant="outline" onClick={onClose}>
						취소
					</Button>
					<Button type="submit" disabled={!isDirty || blocked} aria-busy={pending} className="gap-2">
						<Save size={16} aria-hidden="true" /> 저장
					</Button>
				</div>
			</form>
		</BaseModal>
	);
}
