import { useBusinessMutation } from "@/operations/useBusinessMutation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { operations } from "@/app/bootstrap";
import { successOutcome } from "@/operations/outcome";
import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";
import { QueryNotice } from "@/queries/QueryNotice";
import { useDraft } from "@/editors/useDraft";
import { DraftConflict } from "@/editors/DraftConflict";
import Check from "lucide-react/dist/esm/icons/check.mjs";
import Loader2 from "lucide-react/dist/esm/icons/loader-2.mjs";
import Save from "lucide-react/dist/esm/icons/save.mjs";
import SettingsIcon from "lucide-react/dist/esm/icons/settings.mjs";
import { useRef, useState } from "react";
import { queryKeys } from "@/queries/keys";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { settingsApi } from "@/features/settings/api";
import toast from "@/lib/toast-api";

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

export const SettingsForm = () => {
	const queryClient = useQueryClient();

	const query = useQuery({
		queryKey: queryKeys.settings.all,
		queryFn: settingsApi.get,
	});

	const online = useOnline();
	const view = queryView(query, online, () => false);
	const { data: settingsData, refetch } = query;
	const defaultAlarmMinutes = settingsData?.settings.alarmAdvanceMinutes;
	const knownSettings = defaultAlarmMinutes !== undefined;
	const editor = useDraft(knownSettings ? String(defaultAlarmMinutes) : undefined);
	const alarmAdvanceMinutes = editor.value;
	const isDirty = editor.dirty;
	const [error, setError] = useState("");
	const [saveResult, setSaveResult] = useState("");
	const alarmAdvanceMinutesInputRef = useRef<HTMLInputElement>(null);

	const updateMutation = useBusinessMutation({
		mutationFn: settingsApi.update,
		onSuccess: (result) => {
			queryClient.setQueryData(queryKeys.settings.all, { status: result.status, settings: result.settings });
			void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
			editor.committed(String(result.settings.alarmAdvanceMinutes));
			setError("");
			const outcome = successOutcome("holoUpdateSettings", result);
			const { message } = outcome;
			setSaveResult(message);
			if (outcome.kind !== "succeeded") toast.error(message);
			else toast.success(message);
		},
		onError: (err: Error) => {
			const outcome = operations.failure(err);
			const message = `설정 저장: ${outcome?.message ?? "작업 결과를 확인하지 못했습니다. 현재 상태를 다시 조회해 주세요."}`;
			void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
			setSaveResult(message);
			toast.error(message);
		},
	});

	const onSubmit = (event: React.SyntheticEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!view.current || !knownSettings || !isDirty || editor.conflict || updateMutation.isPending) return;

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
				<QueryNotice view={view} label="시스템 설정" onRetry={() => { void refetch(); }} />
				{editor.conflict && <DraftConflict latest={editor.latest} useLatest={editor.useLatest} keepDraft={editor.keepDraft} />}
				{defaultAlarmMinutes !== undefined && (defaultAlarmMinutes < 1 || defaultAlarmMinutes > 60) && <p role="status">현재 저장값은 {defaultAlarmMinutes}분입니다. 변경할 때는 1~60분 범위에서 입력해 주세요.</p>}
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
											editor.setValue(event.target.value);
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
							disabled={!view.current || !knownSettings || !isDirty || editor.conflict || updateMutation.isPending}
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
							{!view.current || !knownSettings ? "설정 확인 필요" : updateMutation.isPending
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
