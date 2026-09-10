import type { AxiosResponse } from "axios";
import { RequestBlockedError, SessionChangedError } from "@/api/errors";
import contracts from "@/api/generated/operation-contracts";
import type { Operation } from "@/api/transport";
import { errorOutcome, successOutcome, type Outcome } from "@/operations/outcome";

const labels: Record<string, string> = {
	handle_docker_restart: "컨테이너 재시작", handle_docker_start: "컨테이너 시작", handle_docker_stop: "컨테이너 중지",
	holoAddMember: "멤버 추가", holoAddAlias: "별명 추가", holoRemoveAlias: "별명 삭제", holoSetGraduation: "졸업·복귀 변경",
	holoUpdateChannel: "채널 변경", holoUpdateMemberName: "멤버 이름 변경", holoAddRoom: "방 접근 목록 추가", holoRemoveRoom: "방 접근 목록 삭제",
	holoSetAcl: "방 접근 정책 변경", holoUpdateSettings: "설정 저장", holoDeleteAlarm: "알람 삭제", holoSetRoomName: "방 이름 변경", holoSetUserName: "사용자 이름 변경",
};

export interface OperationRecord {
	operationId: string;
	label: string;
	authGeneration: number;
	startedAt: number;
	finishedAt: number;
	outcome: Outcome;
}

export interface OperationSnapshot {
	busy: boolean;
	activeLabel: string | null;
	result: OperationRecord | null;
}

/** Operations는 앱 수명 동안 업무 변경 하나와 마지막 결과를 소유합니다. reload 복원은 하지 않습니다. */
export class Operations {
	private active: { label: string; epoch: number } | null = null;
	private last: OperationRecord | null = null;
	private failures = new WeakMap<object, Outcome>();
	private current: OperationSnapshot = { busy: false, activeLabel: null, result: null };
	private listeners = new Set<() => void>();
	private readonly authGeneration: () => number;
	private readonly online: () => boolean;
	constructor(authGeneration: () => number, online: () => boolean) {
		this.authGeneration = authGeneration; this.online = online;
		const mutations = contracts.filter(operation => operation.mutation);
		if (mutations.length !== Object.keys(labels).length || mutations.some(operation => labels[operation.id] === undefined)) throw new Error("Unclassified administrator mutation");
	}
	snapshot = (): OperationSnapshot => this.current;
	subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };

	/** hideSensitive는 이전 인증의 결과를 숨기되 진행 중 요청을 취소 성공으로 바꾸거나 잠금을 풀지 않습니다. */
	hideSensitive = (): void => { this.publish(); };
	/** dismiss는 표시된 마지막 결과만 닫으며 진행 중 요청에는 영향을 주지 않습니다. */
	dismiss = (): void => { this.last = null; this.publish(); };
	/** failure는 해당 오류의 기록을 반환하여 다른 요청의 마지막 결과와 혼동하지 않게 합니다. */
	failure(error: unknown): Outcome | undefined {
		return typeof error === "object" && error !== null ? this.failures.get(error) : undefined;
	}
	private publish(): void {
		const epoch = this.authGeneration();
		this.current = { busy: this.active !== null, activeLabel: this.active?.epoch === epoch ? this.active.label : null, result: this.last?.authGeneration === epoch ? this.last : null };
		for (const listener of this.listeners) listener();
	}

	/** execute는 BUSY/offline을 즉시 거부하고 전송 시도와 확인된 결과를 기록합니다. 재시도/대기열은 없습니다. */
	async execute<T>(operation: Operation, work: (markDispatched: () => void) => Promise<AxiosResponse<T>>): Promise<AxiosResponse<T>> {
		if (this.active) throw new RequestBlockedError("BUSY", "다른 업무 변경을 처리 중입니다. 완료 후 다시 시도해 주세요.");
		const epoch = this.authGeneration();
		const label = labels[operation.id];
		if (!operation.mutation || label === undefined) throw new Error("Unclassified administrator mutation");
		const startedAt = Date.now();
		let dispatched = false;
		this.active = { label, epoch };
		this.publish();
		try {
			if (!this.online()) throw new RequestBlockedError("OFFLINE", "오프라인에서는 변경 요청을 보내지 않습니다. 연결 후 직접 다시 시도해 주세요.");
			const response = await work(() => {
				if (this.authGeneration() !== epoch) throw new SessionChangedError();
				if (!this.online()) throw new RequestBlockedError("OFFLINE", "연결이 끊겨 요청을 보내지 않았습니다.");
				dispatched = true;
			});
			this.last = { operationId: operation.id, label, authGeneration: epoch, startedAt, finishedAt: Date.now(), outcome: successOutcome(operation.id, response.data) };
			return response;
		} catch (error) {
			const outcome = errorOutcome(error, dispatched);
			if (typeof error === "object" && error !== null) this.failures.set(error, outcome);
			this.last = { operationId: operation.id, label, authGeneration: epoch, startedAt, finishedAt: Date.now(), outcome };
			throw error;
		} finally {
			this.active = null;
			this.publish();
		}
	}
}
