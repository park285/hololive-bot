import { AxiosHeaders, isAxiosError } from "axios";
import { ContractError, RequestBlockedError } from "@/api/errors";
import type { SettingsUpdateResponse } from "@/api/generated/data-contracts";

export type EffectState = "confirmed" | "failed" | "unknown" | "not_needed";
export interface OperationEffect { key: string; label: string; state: EffectState }
export type Outcome = {
	kind: "succeeded" | "partial" | "rejected" | "failed" | "unknown";
	effects: OperationEffect[];
	message: string;
	requestId?: string;
} | {
	kind: "accepted";
	effects: OperationEffect[];
	message: string;
	trackingId: string;
};

function effectState(value: boolean | undefined): EffectState {
	return value === true ? "confirmed" : value === false ? "failed" : "unknown";
}

/** successOutcome은 검증된 소유 API의 성공/부분 효과만 해석합니다. 현재 API에는 accepted 응답이 없습니다. */
export function successOutcome(operation: string, data: unknown): Outcome {
	if (operation === "holoUpdateSettings") {
		// transport가 동일 정본의 SettingsUpdateResponse validator를 통과한 뒤에만 호출합니다.
		const result = data as SettingsUpdateResponse;
		const effects: OperationEffect[] = [
			{ key: "saved", label: "설정 저장", state: "confirmed" },
			{ key: "applied", label: "런타임 적용", state: effectState(result.runtime.alarm_applied) },
			{ key: "published", label: "다른 서비스에 전파", state: effectState(result.runtime.config_publish_alarm_advance_minutes) },
		];
		const complete = effects.every(effect => effect.state === "confirmed");
		const problems: string[] = [];
		if (result.runtime.alarm_applied !== true) problems.push(result.runtime.alarm_applied === false ? "런타임 적용에 실패했습니다." : "런타임 적용 결과를 확인하지 못했습니다.");
		if (result.runtime.config_publish_alarm_advance_minutes !== true) problems.push(result.runtime.config_publish_alarm_advance_minutes === false ? "다른 서비스로 전파하지 못했습니다." : "전파 결과를 확인하지 못했습니다.");
		return { kind: complete ? "succeeded" : "partial", effects, message: complete ? "설정을 저장하고 적용·전파했습니다." : `설정은 저장됐지만 ${problems.join(" ")}` };
	}
	if (operation === "holoDeleteAlarm" && typeof data === "object" && data !== null && "removed" in data && data.removed === false) {
		return { kind: "succeeded", effects: [{ key: "removed", label: "알람 삭제", state: "not_needed" }], message: "해당 방·채널의 알람이 이미 없습니다." };
	}
	return { kind: "succeeded", effects: [{ key: "completed", label: "업무 API 처리", state: "confirmed" }], message: "요청한 변경을 완료했습니다." };
}

/** errorOutcome은 전송 후 확인 불가를 실패로 바꾸지 않고 상관 ID도 receipt로 사용하지 않습니다. */
export function errorOutcome(error: unknown, dispatched: boolean): Outcome {
	if (!dispatched) {
		return { kind: error instanceof ContractError ? "rejected" : "failed", effects: [], message: error instanceof RequestBlockedError ? error.message : "요청을 전송하지 못했습니다. 입력과 연결 상태를 확인해 주세요." };
	}
	if (isAxiosError<unknown>(error) && error.response) {
		const {data} = error.response;
		if (typeof data === "object" && data !== null && "code" in data && "requestId" in data && typeof data.requestId === "string") {
			const mutationID = AxiosHeaders.from(error.config?.headers).get("X-Admin-Mutation-ID");
			// C04/ASVS 2.3.1: 마지막 HTTP 거부만으로 이전 네트워크 시도의 효과를 부정할 수 없습니다.
			if (typeof mutationID === "string" && "notDispatchedMutationId" in data && data.notDispatchedMutationId === mutationID) {
				return { kind: error.response.status < 500 ? "rejected" : "failed", effects: [], message: error.response.status < 500 ? "서버가 요청을 거절했습니다. 사유를 확인한 뒤 새 요청을 결정해 주세요." : "요청을 처리하지 못했으며 업무 변경은 실행되지 않았습니다.", requestId: data.requestId };
			}
			return unknownOutcome(data.requestId);
		}
	}
	return unknownOutcome();
}

function unknownOutcome(requestId?: string): Outcome {
	return { kind: "unknown", effects: [{ key: "completed", label: "업무 효과", state: "unknown" }], message: "작업 결과를 확인하지 못했습니다. 같은 변경을 자동으로 다시 실행하지 않습니다. 현재 상태를 조회한 뒤 새 작업을 결정해 주세요.", ...(requestId ? { requestId } : {}) };
}
