# 사용자 결정 (2026-10-19, 이 세션)

- 범위: 계획 0~4단계 전체. 단위(PR)별 worktree+브랜치, 부모가 로컬 커밋(push·PR 없음).
- 2단계 runtime_state: **F1 + READ COMMITTED**. 트리거 제거, Go가 변경 key를 모아 기존 SQL 계산 함수 refresh_youtube_live_runtime_state(text[])를 트랜잭션 끝에 한 번 명시 호출. runtime 역할 권한 불변. guard는 advisory 잠금으로 대체, retention은 exclusive 유지. record_youtube_live_review의 요구 격리 수준 SERIALIZABLE → READ COMMITTED. (설계: local://apsm-design-S2.md)
- 3단계 youtube: alarm_states.authorized_at claim **제거**(동작 변화 수용: 결과 불명 뒤 같은 post 다른 방 2분 지연이 사라짐, ClaimDeferred·'Alarm claim token' 계약 절 삭제). ClaimToken·Reused·claimDecisionCache·batchClaimResolver 삭제. authorized_at 열 drop은 다음 릴리스 별도 PR. (설계: local://apsm-design-S3B.md)
- 3단계 notification: **(i) 확정**. notification_delivery_outbox 테이블·API producer·rearm·identity·room FIFO 의미 유지, worker 소비자만 공통 fence 기반(sendfence)으로 이전. migration·계약 변경 없음. 4단계 reconciler는 notification 원장에도 어댑터(handoff 확인 열 포함)를 둔다. (설계: local://apsm-design-S3A.md)
- 봇 reconciler 간격: 봇 전용 짧은 ladder(250ms→500ms→1s→2s→5s→15s→60s, 지평 절단). 워커는 5s/15s/60s/5min.
- 4단계 워커: Iris `failed` → **재발급**(generation<2이고 예산이 남으면 retry + generation+1, :r1/:r2). alarm-dispatch·youtube에서도 새 재발송 경로가 생기는 것을 수용. sendoutcome Kind 4종 유지(admission 단계 OutcomeUnknown 필요, 부모 결정). 지평 = 기존 executor.attempt_timeout(새 설정 키 없음). reconciler는 GetReplyStatus failed를 보면 바로 세대를 올림(계획대로). (설계: local://apsm-design-S4.md)
- 4단계 봇: Iris `failed` → **재발급**(retryable_pre_dispatch로 되돌려 :r1/:r2). 상태 어휘 10종 **유지**, 전이 축소, CHECK는 2단계(완화 → 다음 릴리스 엄격). 같은 방 응답 순서 보류 유지(권고 A).
- 운영 프로필(stack-secrets) 값 변경, 운영 migration 적용, 배포, push/PR은 이번 범위 밖(별도 승인).

# migration 파일 번호 배정 (부모)
- 325: 2a(runtime_state shadow key 등) / 328: 2b(트리거·guard 제거)
- 326: 4b-1 열·완화 CHECK, 333: 4b-1 CREATE INDEX CONCURRENTLY / 332: 4b-2 엄격 CHECK(다음 릴리스)
- 327: 3c-1 youtube locked_by / 331: 3c-4 authorized_at drop·ENQUEUED(다음 릴리스)
- 329: 4a alarm send unit handoff 열 / 330: 4a notification handoff 열
- 334·335: 4b-2 submitting lease index 생성·legacy lease index 제거(CONCURRENTLY 단일 문장)
- 336: 4a-2 youtube send_request handoff 열 / 337·338·339: 4a alarm·notification·youtube handoff due index(CREATE INDEX CONCURRENTLY 단일 문장 파일)
- manifest.txt ordinal은 각 브랜치가 185 다음부터 붙이고, 통합 시 부모가 머지 순서대로 다시 맞춘다.
