import Save from "lucide-react/dist/esm/icons/save.mjs";
import Video from "lucide-react/dist/esm/icons/video.mjs";
import { type SyntheticEvent, useEffect, useMemo, useState } from "react";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

interface ChannelEditModalProps {
	isOpen: boolean;
	onClose: () => void;
	onSave: (newChannelId: string) => void;
	memberId: number;
	memberName: string;
	currentChannelId: string;
}

export default function ChannelEditModal({
	isOpen,
	onClose,
	onSave,
	memberId,
	memberName,
	currentChannelId,
}: ChannelEditModalProps) {
	const [channelId, setChannelId] = useState(currentChannelId);
	const [error, setError] = useState("");

	useEffect(() => {
		if (isOpen) {
			setChannelId(currentChannelId);
			setError("");
		}
	}, [currentChannelId, isOpen]);

	const isDirty = useMemo(
		() => channelId.trim() !== currentChannelId.trim(),
		[channelId, currentChannelId],
	);

	const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();

		if (channelId.trim().length < 24) {
			setError("채널 ID 형식이 올바르지 않습니다 (최소 24자).");
			return;
		}

		onSave(channelId.trim());
		onClose();
	};

	const title = (
		<span className="flex items-center gap-2">
			<Video className="text-red-600" size={20} aria-hidden="true" />
			채널 ID 수정
		</span>
	);

	return (
		<BaseModal isOpen={isOpen} onClose={onClose} title={title} showHeaderBorder>
			<form onSubmit={handleSubmit} className="space-y-4">
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
						id="channel-edit-input"
						value={channelId}
						onChange={(event) => {
							setChannelId(event.target.value);
							setError("");
						}}
						placeholder="UC…"
						className="font-mono"
						hasError={!!error}
					/>
					{error && (
						<p className="text-[0.8rem] font-medium text-destructive">
							{error}
						</p>
					)}
				</div>

				<div className="mt-6 flex justify-end gap-3 pt-2">
					<Button type="button" variant="outline" onClick={onClose}>
						취소
					</Button>
					<Button type="submit" disabled={!isDirty} className="gap-2">
						<Save size={16} aria-hidden="true" /> 저장
					</Button>
				</div>
			</form>
		</BaseModal>
	);
}
