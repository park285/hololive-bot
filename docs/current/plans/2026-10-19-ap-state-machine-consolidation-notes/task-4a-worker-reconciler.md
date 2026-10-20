<task>
배정 worktree: /home/kapu/work/iris-stack/hololive-bot-apsm-4a (브랜치 refactor/worker-handoff-reconciler, 169944c93 = 3b(alarm dispatch fence) + 3a(notification sendfence) 병합 위). 모든 편집은 이 경로의 절대 경로로.
CHANGELOG 담당: 예(하위 단계마다 한 항목).

4단계 워커(Iris 202 동기 추상화 B안) 구현. 설계 local://apsm-design-S4.md §1·§2·§3·§5·§6·§7. 사용자 결정 local://apsm-decisions.md(특히 "4단계 워커": failed → 재발급(generation<2이고 예산이 남으면 retry + generation+1, :r1/:r2), reconciler는 GetReplyStatus failed를 보면 바로 세대를 올림(§2.5 계획안), sendoutcome Kind 4종 유지, 지평 = 기존 executor.attempt_timeout(새 설정 키 없음), 워커 ladder 5s→15s→60s→5min 지평 절단).

**설계와 달라진 전제(반드시 반영):**
- S4는 cb105cc97 기준으로 쓰였다. 3b가 alarm dispatch 소유자 전이를 바꿨다: 모든 전이가 (status, locked_by, attempt_count) fence 단일 SQL + 잠금 뒤 clock_timestamp() 하나 + *sendfence.MissError, MarkSent·ReissueSendRequest·BeginSendRequest가 단일 문장, retry_after_ms, NextVisibleAt 제거. §2.3 MarkSent 확장·§2.5 재발급은 **현재(3b) 코드 위에** 얹는다(설계 SQL을 그대로 쓰지 말고 3b 규약에 맞춘다).
- reconciler 적용 단계 fence 미스는 가능하면 sendfence 관측 규약(잠금 뒤 clock 하나, 관측 열, sendfence.Check, MissError)으로 표현한다. 맞지 않는 부분이 있으면 이유를 보고한다. sendfence 공개 API는 바꾸지 않는다(필요하면 먼저 부모에게 묻는다).
- notification은 alarm-dispatch로 흡수되지 **않는다**(사용자 결정 (i)). notification_delivery_outbox는 자체 원장으로 남고 4단계 reconciler 어댑터(handoff 확인 열, migration 330)를 따로 둔다. S4 §2.6의 notification 문단은 무효.
- migration 번호: 4a-1 alarm send unit handoff 열·CHECK = **329**, 그 handoff due index(CREATE INDEX CONCURRENTLY)는 별도 단일 문장 파일 **337**(CONVENTIONS.md: CONCURRENTLY는 트랜잭션 밖 단일 문장 파일). notification 어댑터 = **330** + index **338**. youtube 4a-2 = **336** + index **339**. manifest.txt는 현재 마지막 ordinal 다음에 이어 붙인다(부모가 통합 시 재정렬).

**하위 단계별로 진행. 각 단계 끝에 최종 응답(JSON)으로 멈추고, 부모가 리뷰·local-ci·커밋한 뒤 다음 단계 지시를 보낸다.**
- **4a-1(이번 지시):** S4 §6 표의 4a-1 — migration 329/337, SendPreparedMessage requestId 반환(shared + 호출자), text lane SendMessageAccepted(§1.3·§3 확인), MarkSent 확장(§2.3, 3b 규약), 공유 순수 패키지 W/egress/handoffcheck(decideHandoff·간격 상수, alarm·notification·youtube 공유), alarm-dispatch reconciler(§2.4, 외부 호출은 트랜잭션 밖, SKIP LOCKED soft claim), 재발급(§2.5), CLOCK_QUERY_POLICIES, 시험 T-R1~(§5 4a-1), 계약 문서·runbook·CHANGELOG. **폴링은 유지**한다(4a-3에서 제거).
- 4a-1n(다음 지시 예정): notification 어댑터(330/338).
- 4a-2: youtube 어댑터(336/339) — 3c 수렴(3c-3) 병합 뒤.
- 4a-3: 폴링 제거·sentinel 정리·quarantine 3배 규칙 제거와 관계 검증·alarm.md "Iris Markdown admission" 절 교체·stack 검사.

규칙:
- 공통 규칙 local://apsm-rules.md 를 먼저 끝까지 읽고 전부 따른다(절대 경로, lint, Modern Go, 한국어 주석, 시험 품질).
- 외부 호출(GetReplyStatus)은 트랜잭션 안에 넣지 않는다. 시각은 SQL clock_timestamp() 한 곳에서, Go 시각과 DB 시각을 비교하지 않는다. clock CTE는 인덱스 범위 조건이 필요한 곳에서 CROSS JOIN 말고 스칼라 하위질의(InitPlan)로 참조한다(3b 리뷰에서 실측된 회귀 패턴).
- 새 지표 이름을 만들기 전에 기존 지표 재사용을 우선한다(S4 §2.4).
- 검증: golangci-lint(alarm-worker·shared·dbtest 등 바꾼 모듈) 0 issues, go test -race ./internal/egress/... ./internal/service/alarm/dispatchoutbox/... ./internal/app/workerapp/..., dbtest(-run 'AlarmSendUnitHandoff|TestSchemaSnapshotGolden|Migration'), check-sql-ownership, check-migration-manifest, /home/kapu/work/iris-stack/tools/checks/check-stack-retry-contract.sh·check-stack-reissue-contract.sh·check-stack-worker-contract.sh.
</task>
<constraints>
- 다른 에이전트 위임 금지. 커밋·push 금지(부모가 커밋).
- 최종 응답 JSON: status, summary, design_decision, files_changed, tests, verification[{command,result}], contract_changes, risks, blockers, commit_message(`refactor(worker): 한국어 요약` + 본문).
</constraints>
