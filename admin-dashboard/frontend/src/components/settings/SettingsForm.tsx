import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Check from "lucide-react/dist/esm/icons/check.mjs";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import Save from "lucide-react/dist/esm/icons/save.mjs";
import SettingsIcon from "lucide-react/dist/esm/icons/settings.mjs";
import { useEffect, useMemo, useRef, useState } from "react";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { settingsApi } from "@/features/settings/api";
import type { SettingsResponse } from "@/features/settings/types";
import toast from "@/lib/toast-api";
import { getErrorMessageFromUnknown } from "@/lib/typeUtils";

interface SettingsFormProps {
	initialData?: SettingsResponse;
}

const validateAlarmAdvanceMinutes = (value: string) => {
	const trimmed = value.trim();
	if (!trimmed) return "숫자를 입력해주세요.";

	const parsed = Number(trimmed);
	if (!Number.isFinite(parsed)) return "숫자를 입력해주세요.";
	if (!Number.isInteger(parsed)) return "분 단위 정수로 입력해주세요.";
	if (parsed < 1) return "최소 1분 이상이어야 합니다.";
	if (parsed > 60) return "최대 60분까지만 설정 가능합니다.";
	return "";
};

export const SettingsForm = ({ initialData }: SettingsFormProps) => {
	const queryClient = useQueryClient();

	const { data: settingsData } = useQuery({
		queryKey: queryKeys.settings.all,
		queryFn: settingsApi.get,
		initialData,
	});

	const defaultAlarmMinutes =
		settingsData?.settings.alarmAdvanceMinutes ??
		initialData?.settings.alarmAdvanceMinutes ??
		5;
	const [alarmAdvanceMinutes, setAlarmAdvanceMinutes] = useState(
		String(defaultAlarmMinutes),
	);
	const [error, setError] = useState("");
	const previousDefaultRef = useRef(defaultAlarmMinutes);

	const isDirty = useMemo(
		() => alarmAdvanceMinutes.trim() !== String(defaultAlarmMinutes),
		[alarmAdvanceMinutes, defaultAlarmMinutes],
	);

	useEffect(() => {
		const previousDefault = previousDefaultRef.current;
		previousDefaultRef.current = defaultAlarmMinutes;

		if (alarmAdvanceMinutes.trim() === String(previousDefault)) {
			setAlarmAdvanceMinutes(String(defaultAlarmMinutes));
			setError("");
		}
	}, [alarmAdvanceMinutes, defaultAlarmMinutes]);

	const updateMutation = useMutation({
		mutationFn: settingsApi.update,
		onSuccess: (_, variables) => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
			setAlarmAdvanceMinutes(String(variables.alarmAdvanceMinutes));
			setError("");
			toast.success("설정이 성공적으로 저장되었습니다.");
		},
		onError: (err: Error) => {
			toast.error(`설정 저장 실패: ${getErrorMessageFromUnknown(err)}`);
		},
	});

	const onSubmit = (event: React.SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();

		const nextError = validateAlarmAdvanceMinutes(alarmAdvanceMinutes);
		if (nextError) {
			setError(nextError);
			return;
		}

		updateMutation.mutate({
			alarmAdvanceMinutes: Number(alarmAdvanceMinutes.trim()),
		});
	};

	return (
		<Card className="relative overflow-hidden">
			<div className="absolute top-0 left-0 right-0 h-1 bg-linear-to-r from-sky-400 to-cyan-400" />
			<Card.Header className="flex flex-row items-center gap-2 border-b border-border-subtle pb-4">
				<span className="flex items-center justify-center w-9 h-9 rounded-xl bg-linear-to-br from-sky-400 to-cyan-400 text-white shadow-sm shadow-sky-200/50"><SettingsIcon size={18} aria-hidden="true" /></span>
				<h3 className="text-lg font-bold text-foreground">시스템 설정</h3>
			</Card.Header>

			<Card.Body className="space-y-6 pt-6">
				<form onSubmit={onSubmit} className="space-y-6">
					<div>
						<h4 className="mb-4 border-l-4 border-sky-400 pl-3 text-sm font-bold text-foreground">
							알림 옵션
						</h4>

						<div className="rounded-lg border border-border-subtle bg-muted p-5 transition-colors hover:border-border focus-within:ring-2 focus-within:ring-sky-100">
							<div className="space-y-2">
								<Label htmlFor="alarm-advance-minutes">
									알람 사전 알림 시간
								</Label>
								<div className="flex items-center gap-3">
									<Input
										id="alarm-advance-minutes"
										type="number"
										min={1}
										max={60}
										step={1}
										inputMode="numeric"
										value={alarmAdvanceMinutes}
										onChange={(event) => {
											setAlarmAdvanceMinutes(event.target.value);
											setError("");
										}}
										className="w-24 bg-card text-center font-bold tabular-nums focus-visible:ring-2 focus-visible:ring-sky-200"
										hasError={!!error}
									/>
									<span className="text-sm font-medium text-muted-foreground">
										분 전 알림
									</span>
								</div>
								<p className="text-[0.8rem] text-muted-foreground">
									방송 시작 몇 분 전에 채팅방으로 알람을 전송할지 설정합니다.
								</p>
								{error && (
									<p className="text-[0.8rem] font-medium text-destructive">
										{error}
									</p>
								)}
							</div>
						</div>
					</div>

					<div className="flex justify-end pt-2">
						<Button
							type="submit"
							disabled={!isDirty || updateMutation.isPending}
							className="gap-2 bg-linear-to-r from-sky-500 to-cyan-500 hover:from-sky-600 hover:to-cyan-600 shadow-sm shadow-sky-200 focus-visible:ring-2 focus-visible:ring-sky-200"
							aria-label="설정 저장하기"
						>
							{updateMutation.isPending ? (
								<Loader2
									size={16}
									className="animate-spin"
									aria-hidden="true"
								/>
							) : isDirty ? (
								<Save size={16} aria-hidden="true" />
							) : (
								<Check size={16} aria-hidden="true" />
							)}
							{updateMutation.isPending
								? "저장 중…"
								: isDirty
									? "변경 사항 저장"
									: "저장됨"}
						</Button>
					</div>
				</form>
			</Card.Body>
		</Card>
	);
};
