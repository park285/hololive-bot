import Save from "lucide-react/dist/esm/icons/save.mjs";
import Video from "lucide-react/dist/esm/icons/video.mjs";
import { type ReactNode, type SyntheticEvent, useRef, useState } from "react";
import { useDraft } from "@/editors/useDraft";
import { DraftConflict } from "@/editors/DraftConflict";
import { RequestBlockedError } from "@/api/errors";
import { operations } from "@/app/bootstrap";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

interface ChannelEditModalProps {
	isOpen: boolean;
	onClose: () => void;
	onSave: (newChannelId: string) => Promise<void>;
	canSubmit: boolean;
	pending: boolean;
	readState: ReactNode;
	memberId: string;
	memberName: string;
	currentChannelId: string | undefined;
}

export default function ChannelEditModal({
	isOpen,
	onClose,
	onSave,
	canSubmit,
	pending,
	readState,
	memberId,
	memberName,
	currentChannelId,
}: ChannelEditModalProps) {
	const draft = useDraft(currentChannelId);
	const channelId = draft.value;
	const [error, setError] = useState("");
	const channelIdInputRef = useRef<HTMLInputElement>(null);

	const isDirty = draft.dirty;
	const blocked = !canSubmit || pending || draft.conflict;

	const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (blocked) return;

		if (channelId.trim().length < 24) {
			setError("채널 ID 형식이 올바르지 않습니다 (최소 24자).");
			queueMicrotask(() => {
				channelIdInputRef.current?.focus();
			});
			return;
		}

		void onSave(channelId.trim()).catch((cause: unknown) => {
			setError(operations.failure(cause)?.message ?? (cause instanceof RequestBlockedError ? cause.message : "작업 결과를 확인하지 못했습니다. 현재 상태를 다시 조회해 주세요."));
		});
	};

	const title = (
		<span className="flex items-center gap-2">
			<Video className="text-red-600" size={20} aria-hidden="true" />
			채널 ID 수정
		</span>
	);

	return (
		<BaseModal isOpen={isOpen} onClose={onClose} title={title} showHeaderBorder>
			<form onSubmit={handleSubmit} className="space-y-4" noValidate>
				{readState}
				{draft.conflict && <DraftConflict latest={draft.latest} useLatest={draft.useLatest} keepDraft={draft.keepDraft} />}
				<div className="mb-4 space-y-2 rounded-lg border border-border-subtle bg-muted p-3">
					<div className="flex justify-between text-sm">
						<span className="text-muted-foreground">멤버 이름</span>
						<span className="font-bold text-foreground">{memberName}</span>
					</div>
					<div className="flex justify-between text-sm">
						<span className="text-muted-foreground">멤버 ID</span>
						<span className="font-mono text-muted-foreground">{memberId}</span>
					</div>
				</div>

				<div className="space-y-2">
					<Label htmlFor="channel-edit-input">YouTube 채널 ID</Label>
					<Input
						ref={channelIdInputRef}
						id="channel-edit-input"
						name="channelId"
						autoComplete="off"
						spellCheck={false}
						value={channelId}
						onChange={(event) => {
							draft.setValue(event.target.value);
							setError("");
						}}
						placeholder="UC…"
						className="font-mono"
						hasError={!!error}
						aria-invalid={!!error}
						aria-describedby={error ? "channel-edit-error" : undefined}
					/>
					{error && (
						<p
							id="channel-edit-error"
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
