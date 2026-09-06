import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { isAxiosError } from "axios";
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

	const { data: settingsData, isPending, isError, refetch } = useQuery({
		queryKey: queryKeys.settings.all,
		queryFn: settingsApi.get,
		initialData,
	});

	const defaultAlarmMinutes = settingsData?.settings.alarmAdvanceMinutes;
	const knownSettings = defaultAlarmMinutes !== undefined;
	const baseline = knownSettings ? String(defaultAlarmMinutes) : "";
	const [alarmAdvanceMinutes, setAlarmAdvanceMinutes] = useState(
		baseline,
	);
	const [error, setError] = useState("");
	const [saveResult, setSaveResult] = useState("");
	const previousDefaultRef = useRef(baseline);
	const alarmAdvanceMinutesInputRef = useRef<HTMLInputElement>(null);

	const isDirty = useMemo(
		() => knownSettings && alarmAdvanceMinutes.trim() !== baseline,
		[alarmAdvanceMinutes, baseline, knownSettings],
	);

	useEffect(() => {
		const previousDefault = previousDefaultRef.current;
		previousDefaultRef.current = baseline;

		if (alarmAdvanceMinutes.trim() === previousDefault) {
			setAlarmAdvanceMinutes(baseline);
			setError("");
		}
	}, [alarmAdvanceMinutes, baseline]);

	const updateMutation = useMutation({
		mutationFn: settingsApi.update,
		retry: false,
		onSuccess: (result) => {
			queryClient.setQueryData(queryKeys.settings.all, { status: result.status, settings: result.settings });
			void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
			setAlarmAdvanceMinutes(String(result.settings.alarmAdvanceMinutes));
			setError("");
			const { runtime } = result;
			const problems: string[] = [];
			if (runtime.alarm_applied !== true) problems.push(runtime.alarm_applied === false ? "런타임 적용에 실패했습니다." : "런타임 적용 결과를 확인하지 못했습니다.");
			if (runtime.config_publish_alarm_advance_minutes !== true) problems.push(runtime.config_publish_alarm_advance_minutes === false ? "다른 서비스로 전파하지 못했습니다." : "전파 결과를 확인하지 못했습니다.");
			const message = problems.length > 0 ? `설정은 저장됐지만 ${problems.join(" ")}` : "설정을 저장하고 적용·전파했습니다.";
			setSaveResult(message);
			if (problems.length > 0) toast.error(message);
			else toast.success(message);
		},
		onError: (err: Error) => {
			const refused = isAxiosError(err) && [400, 401, 403].includes(err.response?.status ?? 0);
			const message = refused ? "설정 저장 요청이 거절되어 변경되지 않았습니다." : `설정 저장 결과를 확인하지 못했습니다: ${getErrorMessageFromUnknown(err)}`;
			setSaveResult(message);
			toast.error(message);
		},
	});

	const onSubmit = (event: React.SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!knownSettings || !isDirty || updateMutation.isPending) return;

		const nextError = validateAlarmAdvanceMinutes(alarmAdvanceMinutes);
		if (nextError) {
			setError(nextError);
			queueMicrotask(() => {
				alarmAdvanceMinutesInputRef.current?.focus();
			});
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
				{isPending && <p role="status">설정을 불러오는 중입니다.</p>}
				{isError && <div role="alert"><p>{knownSettings ? "최신 설정을 조회하지 못했습니다. 마지막으로 확인한 값을 표시합니다." : "설정을 조회하지 못했습니다. 저장된 값을 확인한 뒤 변경할 수 있습니다."}</p><Button type="button" onClick={() => { void refetch(); }}>다시 조회</Button></div>}
				{saveResult && <p role="status">{saveResult}</p>}
				<form onSubmit={onSubmit} className="space-y-6" noValidate>
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
										ref={alarmAdvanceMinutesInputRef}
										id="alarm-advance-minutes"
										name="alarmAdvanceMinutes"
										autoComplete="off"
										type="number"
										min={1}
										max={60}
										step={1}
										inputMode="numeric"
										value={alarmAdvanceMinutes}
										disabled={!knownSettings || updateMutation.isPending}
										onChange={(event) => {
											setAlarmAdvanceMinutes(event.target.value);
											setError("");
										}}
										className="w-24 bg-card text-center font-bold tabular-nums focus-visible:ring-2 focus-visible:ring-sky-200"
										hasError={!!error}
										aria-invalid={!!error}
										aria-describedby={
											error
												? "alarm-advance-minutes-help alarm-advance-minutes-error"
												: "alarm-advance-minutes-help"
										}
									/>
									<span className="text-sm font-medium text-muted-foreground">
										분 전 알림
									</span>
								</div>
								<p
									id="alarm-advance-minutes-help"
									className="text-[0.8rem] text-muted-foreground"
								>
									방송 시작 몇 분 전에 채팅방으로 알람을 전송할지 설정합니다.
								</p>
								{error && (
									<p
										id="alarm-advance-minutes-error"
										role="status"
										aria-live="polite"
										className="text-[0.8rem] font-medium text-destructive"
									>
										{error}
									</p>
								)}
							</div>
						</div>
					</div>

					<div className="flex justify-end pt-2">
						<Button
							type="submit"
							disabled={!knownSettings || !isDirty || updateMutation.isPending}
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
							{!knownSettings ? "설정 확인 필요" : updateMutation.isPending
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
