import Save from "lucide-react/dist/esm/icons/save.mjs";
import UserPlus from "lucide-react/dist/esm/icons/user-plus.mjs";
import {
	type SyntheticEvent,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { BaseModal } from "@/components/ui/BaseModal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

interface AddMemberFormValues {
	name: string;
	channelId: string;
	nameKo?: string;
	nameJa?: string;
}

interface AddMemberModalProps {
	isOpen: boolean;
	onClose: () => void;
	onAdd: (member: AddMemberFormValues) => void;
}

interface AddMemberErrors {
	name?: string;
	channelId?: string;
}

const initialValues: AddMemberFormValues = {
	name: "",
	channelId: "",
	nameKo: "",
	nameJa: "",
};

const validate = (values: AddMemberFormValues): AddMemberErrors => {
	const errors: AddMemberErrors = {};

	if (!values.name.trim()) {
		errors.name = "멤버 이름을 입력해주세요.";
	}

	if (values.channelId.trim().length < 24) {
		errors.channelId = "ID 형식이 올바르지 않습니다 (최소 24자).";
	}

	return errors;
};

export default function AddMemberModal({
	isOpen,
	onClose,
	onAdd,
}: AddMemberModalProps) {
	const [values, setValues] = useState<AddMemberFormValues>(initialValues);
	const [errors, setErrors] = useState<AddMemberErrors>({});
	const nameInputRef = useRef<HTMLInputElement>(null);
	const channelIdInputRef = useRef<HTMLInputElement>(null);

	useEffect(() => {
		if (isOpen) {
			setValues(initialValues);
			setErrors({});
		}
	}, [isOpen]);

	const isDirty = useMemo(
		() =>
			[
				values.name,
				values.channelId,
				values.nameKo ?? "",
				values.nameJa ?? "",
			].some((value) => value.trim() !== ""),
		[values],
	);

	const handleChange = (field: keyof AddMemberFormValues, value: string) => {
		setValues((prev) => ({ ...prev, [field]: value }));
		setErrors((prev) => ({ ...prev, [field]: undefined }));
	};

	const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();

		const nextErrors = validate(values);
		if (Object.keys(nextErrors).length > 0) {
			setErrors(nextErrors);
			queueMicrotask(() => {
				if (nextErrors.name) {
					nameInputRef.current?.focus();
				} else if (nextErrors.channelId) {
					channelIdInputRef.current?.focus();
				}
			});
			return;
		}

		onAdd({
			name: values.name.trim(),
			channelId: values.channelId.trim(),
			nameKo: values.nameKo?.trim() ?? "",
			nameJa: values.nameJa?.trim() ?? "",
		});
		onClose();
	};

	const title = (
		<span className="flex items-center gap-2">
			<UserPlus className="text-sky-600" size={20} aria-hidden="true" />새 멤버
			추가
		</span>
	);

	return (
		<BaseModal
			isOpen={isOpen}
			onClose={onClose}
			title={title}
			maxWidth="lg"
			showHeaderBorder
		>
			<form onSubmit={handleSubmit} className="space-y-4" noValidate>
				<div className="space-y-2">
					<Label htmlFor="add-member-name">멤버 이름 (기본)</Label>
					<Input
						ref={nameInputRef}
						id="add-member-name"
						name="name"
						autoComplete="off"
						value={values.name}
						onChange={(event) => {
							handleChange("name", event.target.value);
						}}
						placeholder="예: Hoshimachi Suisei"
						className="focus-visible:ring-2 focus-visible:ring-sky-200"
						hasError={!!errors.name}
						aria-invalid={!!errors.name}
						aria-describedby={errors.name ? "add-member-name-error" : undefined}
					/>
					{errors.name && (
						<p
							id="add-member-name-error"
							role="status"
							aria-live="polite"
							className="text-[0.8rem] font-medium text-destructive"
						>
							{errors.name}
						</p>
					)}
				</div>

				<div className="space-y-2">
					<Label htmlFor="add-member-channel-id">YouTube 채널 ID</Label>
					<Input
						ref={channelIdInputRef}
						id="add-member-channel-id"
						name="channelId"
						autoComplete="off"
						spellCheck={false}
						value={values.channelId}
						onChange={(event) => {
							handleChange("channelId", event.target.value);
						}}
						placeholder="UC…"
						className="font-mono focus-visible:ring-2 focus-visible:ring-sky-200"
						hasError={!!errors.channelId}
						aria-invalid={!!errors.channelId}
						aria-describedby={errors.channelId ? "add-member-channel-id-error" : undefined}
					/>
					{errors.channelId && (
						<p
							id="add-member-channel-id-error"
							role="status"
							aria-live="polite"
							className="text-[0.8rem] font-medium text-destructive"
						>
							{errors.channelId}
						</p>
					)}
				</div>

				<div className="grid grid-cols-2 gap-4">
					<div className="space-y-2">
						<Label htmlFor="add-member-name-ko" className="text-muted-foreground">
							한국어 이름 (선택)
						</Label>
						<Input
							id="add-member-name-ko"
							name="nameKo"
							autoComplete="off"
							value={values.nameKo ?? ""}
							onChange={(event) => {
								handleChange("nameKo", event.target.value);
							}}
							placeholder="예: 호시마치 스이세이"
							className="focus-visible:ring-2 focus-visible:ring-sky-200"
						/>
					</div>

					<div className="space-y-2">
						<Label htmlFor="add-member-name-ja" className="text-muted-foreground">
							일본어 이름 (선택)
						</Label>
						<Input
							id="add-member-name-ja"
							name="nameJa"
							autoComplete="off"
							value={values.nameJa ?? ""}
							onChange={(event) => {
								handleChange("nameJa", event.target.value);
							}}
							placeholder="예: 星街すいせい"
							className="focus-visible:ring-2 focus-visible:ring-sky-200"
						/>
					</div>
				</div>

				<div className="mt-6 flex justify-end gap-3 pt-2">
					<Button
						type="button"
						variant="outline"
						onClick={onClose}
						className="focus-visible:ring-2 focus-visible:ring-border"
					>
						취소
					</Button>
					<Button
						type="submit"
						disabled={!isDirty}
						className="gap-2 bg-sky-600 hover:bg-sky-700 shadow-sm shadow-sky-200 focus-visible:ring-2 focus-visible:ring-sky-200"
						aria-label="새 멤버 정보 저장 및 추가"
					>
						<Save size={16} aria-hidden="true" /> 추가하기
					</Button>
				</div>
			</form>
		</BaseModal>
	);
}
