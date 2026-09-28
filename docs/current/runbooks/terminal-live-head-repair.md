# 과거 종료 세션 대조 상태 복구

`DEC-20260928-terminal-live-head-alignment`의 제한된 운영 도구입니다. 이미 종료된 정본 세션을 기준으로 과거 LIVE head의 표시 상태와 종료 시각만 일치시킵니다. 새 방송 종료 판정, observation replay 또는 알림 발송 경로가 아닙니다.

## 대상과 보호 조건

- `youtube_live_sessions.status=ENDED`, 저장된 `ended_at` 존재.
- `youtube_live_reconciliation_heads.status=LIVE`, 마지막 LIVE positive 존재.
- 정본 종료 시각이 마지막 LIVE/UPCOMING positive보다 이전이 아님.
- head `updated_at`이 2026-08-17 이전이며 종료 시각·사유·진행 중 종료 candidate가 모두 비어 있음.
- 최대 100건. 미리 보존한 count/digest가 잠금 이후 현재 스냅샷과 정확히 같아야 함.
- 적용 DB는 `hololive`이며 대상 테이블에 검토하지 않은 사용자 trigger/rule이 있으면 거절함.

정본 session과 head를 각각 video_id 순서로 잠급니다. 대기 중 상태 변경은 다시 검사합니다. lock 3초, statement 10초 상한이며 실패하면 전체 transaction을 취소합니다. head의 `status`, `ended_at`, 기술적 `updated_at`만 갱신하고 나머지 필드는 transaction 안에서 동일성을 검증합니다. 기존 관측 clock, pending 사실, end_reason, canonical session과 dispatch 원장은 변경하지 않습니다.

## 실행 절차

1. owning ops/PG runbook과 현재 대상·효과 승인을 확인합니다. `default_transaction_read_only=on`을 증명한 연결에서 `scripts/runtime/preview-terminal-live-heads.sql`을 실행합니다. 결과의 rows는 operational identifier를 포함하므로 공유 출력에 노출하지 않고 mode 0700 디렉터리의 0600 파일에 저장합니다.
2. count/digest와 모든 원본 행을 검토합니다. 2026-09-28의 승인·실제 적용 범위는 24건이었습니다. 다른 후보를 과거 승인에 포함하지 않습니다.
3. `bash scripts/runtime/test-terminal-live-head-repair.sh`를 kapu에서 실행합니다. 설치된 `postgres:18.6-alpine`만 사용하며 네트워크/host port 없는 테스트 컨테이너를 종료 시 제거합니다.
4. 승인된 쓰기 연결에서 아래 SQL을 **한 번** 실행합니다. 본문과 expected 값은 검증한 파일/preview에서 사용합니다. 권한 상승이나 재시도는 도구가 수행하지 않습니다.

   ```text
   psql --no-psqlrc -v ON_ERROR_STOP=1 \
     -v expected_count=REVIEWED_COUNT -v expected_digest=REVIEWED_DIGEST \
     -f scripts/runtime/reconcile-terminal-live-heads.sql
   ```

5. 성공 exit와 commit 완료를 확인한 후 같은 ID의 head/정본을 새 읽기 전용 연결에서 조회합니다. 정확한 세 필드의 변경, 정본 종료 시각, 비대상과 dispatch 상태를 확인합니다. 전후 snapshot을 함께 보존합니다.
6. 오류/연결 단절은 재실행 근거가 아닙니다. 현재 행과 receipt로 commit 여부를 먼저 확인합니다. 복구가 필요하면 적용 후 스냅샷 전체가 아직 일치하는지 잠금 아래 CAS 검증한 뒤 보존된 원본의 세 필드만 돌립니다. 후속 변경을 덮지 않으며 별도 승인·검증 없이 즉시 rollback하지 않습니다.

2026-09-28 적용 후 mismatch는 27→3이었습니다. 남은 3건은 head 없는 과거 UPCOMING이며 이 도구의 대상이 아닙니다. 예정 시각 경과만으로 ENDED를 만들지 않습니다. Fallback delta: none.
