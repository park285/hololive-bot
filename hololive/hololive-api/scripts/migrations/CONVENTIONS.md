# 마이그레이션 작성 규약

2026-07 SQL 리뷰(스키마 검진 + 문장 시간복잡도 비판)의 처방을 규약화한 것.
`scripts/architecture/check-migration-manifest.sh`가 일부를 기계 검증한다.

## 러너 의미론 (모든 규칙의 전제)

- `apply-all.sh`는 파일 단위 `psql -f`(statement별 autocommit, `-1` 미사용) + `schema_migrations` ledger.
  적용 완료 파일은 skip, **파일 중간 실패 시 ledger 미기록 → 재실행 때 그 파일 전체가 재적용**된다.
- 따라서: ① 모든 문장은 멱등이어야 한다(`IF [NOT] EXISTS`, `NOT VALID` 재적용 가드, 조건부 DO 블록).
  ② `CREATE/DROP INDEX CONCURRENTLY` 사용 가능(트랜잭션 래핑 없음). ③ `BEGIN;/COMMIT;`으로 묶인 파일에는
  CONCURRENTLY를 넣을 수 없다.
- 과거에 적용된 파일의 수정은 프로덕션에 영향이 없고(ledger skip) fresh bootstrap/dbtest 경로만 바꾼다.
  프로덕션을 바꾸려면 항상 새 번호의 파일을 추가한다.

## 번호

- 새 파일은 `max(기존 번호)+1`. 번호 프리픽스 중복 금지(045/051/053은 병행 브랜치 유산으로 예외 — lint가 강제).
- 실행 순서의 SSOT는 manifest.txt이며 파일명 정렬이 아니다.

## 락과 시간복잡도 (문장 품질)

DB에서 문장 비용은 세 축으로 본다: 읽기 복잡도(몇 행을 만지나) / 쓰기 증폭(행 1개 변경이
인덱스 포함 몇 번의 쓰기가 되나) / **락 보유 시간**(다른 트랜잭션을 얼마나 세워두나).
마이그레이션에서는 셋째가 가장 치명적이다.

### SET NOT NULL — 무방비 사용 금지 (lint 강제)

`ALTER COLUMN … SET NOT NULL`은 ACCESS EXCLUSIVE 락(모든 접근 차단)을 쥔 채 전 행을 스캔한다.
유효한 CHECK가 선재하면 PG가 스캔을 생략하므로 다음 레시피를 쓴다:

```sql
ALTER TABLE t ADD CONSTRAINT t_col_nn CHECK (col IS NOT NULL) NOT VALID;  -- 락 순간적
ALTER TABLE t VALIDATE CONSTRAINT t_col_nn;                               -- SHARE UPDATE EXCLUSIVE (쓰기 허용)
ALTER TABLE t ALTER COLUMN col SET NOT NULL;                              -- 유효 CHECK 존재 → 스캔 생략
ALTER TABLE t DROP CONSTRAINT t_col_nn;
```

PG 18에서는 NOT NULL 제약을 `NOT VALID`로 직접 추가해 2단계로 줄일 수 있다.

### 대형 backfill — 단일 UPDATE 한 방 금지

문장 하나 = 트랜잭션 하나: 대상 전 행 락을 문장 끝까지 보유(O(N)), WAL 스파이크, dead tuple 일시불.
keyset 배치 루프로 쪼갠다(러너가 statement별 autocommit이라 궁합이 맞다):

```sql
-- 0 rows가 나올 때까지 반복
UPDATE big_table t
SET    col = …
WHERE  t.id IN (
    SELECT id FROM big_table
    WHERE col IS NULL
    ORDER BY id
    LIMIT 5000
);
```

### 병합/정규화 UPDATE — IS DISTINCT FROM 가드 필수

PG의 UPDATE는 값이 같아도 새 행 버전을 쓴다(dead tuple + 전 인덱스 갱신 후보).
바뀌는 행만 만진다 (062의 선례):

```sql
WHERE m.slug = s.slug
  AND (m.a, m.b) IS DISTINCT FROM (s.a, s.b)
```

같은 비싼 계산(정규화 + 상관 EXISTS)을 여러 문장이 반복하면 `CREATE TEMP TABLE … AS`로 1회 계산 후 재사용.

### 시드 — multi-VALUES 한 문장 + 명시적 멱등 가드

INSERT를 문장 단위로 쪼개면 커밋(fsync) N회 + 중간 실패 시 부분 적용이다. VALUES 리스트 한 문장으로 쓰고,
멱등 가드는 **대상 없는 `ON CONFLICT DO NOTHING`을 금지**하고 `ON CONFLICT (arbiter)` 또는
`INSERT … SELECT … WHERE NOT EXISTS`(064/068/016/017/018 참조)를 쓴다.
members 시드의 arbiter는 `idx_members_slug`(UNIQUE, 097 복원)다.

### 보존(retention) 삭제

인덱스가 많은 테이블의 대량 DELETE는 힙 N + 인덱스 엔트리 N×인덱스수 + vacuum 후불이다.
주기 삭제는 배치 루프(위 keyset)로, 대량·정기 보존은 파티셔닝(파티션 DROP은 O(1))을 검토한다
— `docs/current/architecture/outbox-v3-convergence-roadmap-20260703.md`의 Phase 3 참고.

## 스키마 문서

통합 스키마 문서는 손으로 쓰지 않는다. `hololive/hololive-shared/pkg/dbtest/testdata/schema_snapshot.golden.sql`
(pg_catalog 직렬화 골든 — enum·table·column·constraint·index)이 문서이며 `TestSchemaSnapshotGolden`이
manifest 전체 적용 결과와의 드리프트를 차단한다. pg_dump 출력이 아니라 catalog 직렬화인 이유:
dbtest가 PG18→16 폴백을 갖는데 pg_dump 텍스트는 버전 간 비결정적이다.
갱신: `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-shared/pkg/dbtest`.

## 스키마 표준

컬럼 타입은 세 원칙으로 통일한다(098 기준). ① 내부 생성 어휘 컬럼(status·kind·delivery_status·alarm_type)은 값 집합을
`chk_<table>_<col>_vocab` CHECK로 못박는다 — 폭 상한이 아니라 값 자체가 계약이고, 코드 상수 세트로부터 전수 검증된
값만 기록되기 때문이다(alarm_dispatch_deliveries.status의 TEXT+CHECK 선례와 동형). TEXT 전환은 098(kind 전 컬럼·
major_events.status)과 099(잔여 status·delivery_status·alarm_type 8컬럼)로 완결됐다. ② 사용자 입력이 닿는 표시명은 VARCHAR 폭을 통일한다 —
member_name=TEXT, room_name=VARCHAR(255)(subscription 테이블과 정렬). ③ 표준 식별자 폭(channel_id=64·room_id=100)과
video_id/post_id·alarm_type ENUM·content_id는 건드리지 않는다. VARCHAR→TEXT와 VARCHAR 확대는 테이블 무-rewrite지만
해당 컬럼 인덱스는 재빌드된다. TEXT→VARCHAR·축소는 금지한다.
