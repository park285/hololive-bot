# 과거 종료 세션 대조 상태 복구

**Decisions:** `DEC-20260928-terminal-live-head-alignment` (governing), `DEC-20260926-hololive-live-absence-evidence` (constraint)

## Execution capsule

**Goal:** 정본 ENDED와 과거 LIVE head의 불일치 24건을 알림 재발송 없이 복구한다.
**Context:** 2026-08-15~16 기록 24건은 정본 종료 시각이 모두 마지막 LIVE positive 이후이며 head의 종료 시각·사유·candidate는 비어 있다.
**Constraints:** 정본과 source/pending 사실·event clock·알림 원장을 변경하지 않는다. 3건의 head 없는 UPCOMING은 유지한다. 대상 스냅샷이 달라지면 전체 거절한다.
**Evidence:** 읽기 전용 PostgreSQL 대조, sourceobservation/live_state.go의 정본 세션 우선 정책, 기존 terminal 유지 계약.
**Success:** 선택한 24개 head만 정본의 ENDED·ended_at과 일치하고 mismatch 지표가 27→3으로 줄며 재발송·queue 변화가 없다.
**Output:** bounded SQL 도구·실제 PostgreSQL 회귀 검사·private 복구 스냅샷·docs/review/2026-09-28-terminal-live-head-repair.md.

## 실행

### T01 도구와 사전 증거 준비

Hololive scripts/runtime에 read-only preview와 명시적 count/digest 적용 SQL을 추가한다. 기존 PostgreSQL 18 테스트 이미지로 성공·스냅샷 변경 거절·비대상 보존을 검증한다. 운영 preview는 mandatory read-only guard를 사용하고 원본 head 및 canonical ended_at만 private 로컬 파일에 보존한다.

### T02 원자 적용과 검증

승인된 hololive-osaka/holo-postgres/hololive에서 대상 정본→head를 video_id 순서로 잠그고 snapshot digest와 24건을 재검사한다. lock 3초/statement 10초 제한을 지키며 일괄 적용한다. 새 스냅샷과 지표를 확인한다. 실패/불명은 재실행하지 않고 commit 여부부터 조사한다. 복구는 저장된 원본과 적용 후 CAS가 모두 일치할 때만 별도 원자 transaction으로 수행한다.

## 수용 조건

### AC01 정확한 복구 범위

선택한 24개 head의 status/ended_at/updated_at 외 필드는 보존하고 정본과 pending/dispatch 원장은 변경하지 않는다. 3건의 UPCOMING 누락은 남는다.

### AC02 증거와 복구 가능성

원본·후속 스냅샷과 count/digest/시각을 보존하고 변경 전후 결과를 기록한다. 실패하면 부분 성공을 주장하지 않는다.

## 검증

### V01 실제 PostgreSQL 회귀

격리된 테스트 컨테이너에서 일치하는 snapshot의 정확한 갱신, 잘못된 count/digest 및 새 positive/상태 변경 거절, 비대상/정본/원장 보존을 확인한다.

### V02 운영 후속 검증

read-only guard 아래 count/digest와 field diff를 확인하고 scrape mismatch=3, 기존 격리/DEAD 보존, fresh readiness를 확인한다. Fallback delta: none.
