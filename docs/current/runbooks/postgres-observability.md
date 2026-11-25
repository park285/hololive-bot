# PostgreSQL optional observability

## 범위

`hololive-seoul` (`100.100.1.8`)의 `holo-postgres`/`hololive`에서 쓰는 진단 확장과 preload를 다룬다.
공식 PostgreSQL 19 image의 contrib만 쓴다. preload는 `pg_stat_statements`(unit 기본값)와 선택적
`auto_explain`이고, 전용 스키마 `hololive_observability`에는 `pg_buffercache`·`amcheck`와
`statement_info` view가 있다. 버퍼 조회는 `pg_monitor`, evict 함수와 amcheck 검사는 관리자만 실행할 수 있다.
별도 빌드가 필요했던 `pg_stat_kcache`·`pg_wait_sampling`은 자체 PostgreSQL image와 함께 소스에서 제거했다.

운영 설정 쓰기·재시작·DROP EXTENSION은 명시적으로 승인된 뒤 `hololive-bot-ops`와 `stack-platform-ops` 절차로
수행한다. 기존 volume에 initdb를 재실행하지 않는다. `postgres-replication.md`의 single-primary 경계와 현재
`DEPLOYMENT_BASELINE.md`를 유지한다.

## 공식 image 전환 전 확장 제거

서울 primary는 아직 이전 자체 image에서 두 확장과 preload를 켠 상태다. 공식 image에는 두 모듈이 없으므로
아래 순서를 지킨다. 각 단계는 따로 승인받는다.

1. 이전 image가 실행 중일 때 관리자 socket으로 두 확장과 그에 의존하는 view만 제거한다.
   `pg_buffercache`·`amcheck`·`statement_info`와 스키마는 유지한다. 앱 테이블과 `pg_stat_statements`는 바뀌지 않는다.

   ```bash
   sudo podman exec -i holo-postgres psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 <<'SQL'
   BEGIN;
   SET LOCAL lock_timeout = '2s';
   SET LOCAL statement_timeout = '10s';
   DROP VIEW IF EXISTS hololive_observability.statement_resources RESTRICT;
   DROP EXTENSION IF EXISTS pg_wait_sampling RESTRICT;
   DROP EXTENSION IF EXISTS pg_stat_kcache RESTRICT;
   COMMIT;
   SQL
   ```

   `RESTRICT`가 외부 의존 객체 때문에 실패하면 중단하고 그 객체를 검토한다. `CASCADE`로 우회하지 않는다.
2. `stack-secrets/hosts/hololive-seoul/hololive-bot/compose.env` 마스터의
   `HOLOLIVE_POSTGRES_PRELOAD_LIBRARIES`를 `pg_stat_statements,auto_explain`으로 바꿔 sync하고, **이전 image 그대로**
   `holo-postgres.service`를 재시작한다. `SHOW shared_preload_libraries`와 데이터 volume의
   `postgresql.auto.conf`에 두 모듈이 없음을 확인한다. 두 모듈이 남은 채 공식 image로 바꾸면 시작에 실패한다.
3. 새 unit 파일을 설치한 뒤 공식 image를 load하고, compose.env의 `POSTGRES_IMAGE`를 그 `sha256:` ID로 바꿔
   재시작한다([Quadlet README](../../../deploy/quadlet/README.md#이미지)의 image identity 순서). 이전 값은 롤백용으로 기록한다.
4. 1단계 이전에 뜬 dump는 두 확장을 참조하므로 공식 image에 복원되지 않는다. 그 dump가 보존 기간에서 모두 빠질 때까지
   이전 image를 지우지 않는다. 백업 복원 검증의 공식 image 전환은 1단계 이후 첫 dump부터 적용한다.

## auto_explain

`holo-postgres.container`의 `Exec=`는 1초 이상 문장을 nested 포함으로 기록하며 `log_analyze`·`log_buffers`는 켜고
`log_timing`은 끈다. 바인드 값은 statement log와 같이 남기지 않는다(`log_parameter_max_length=0`). preload에 없으면
설정은 적용되지 않는다. 끌 때는 compose.env의 preload에서 `auto_explain`을 빼고 같은 image로 재시작한다.
ALTER SYSTEM으로 바꾸지 않는다.

## 관측

- `pg_buffercache_summary()`·`pg_buffercache_usage_counts()`는 공유 버퍼 전체를 한 번 훑는
  짧은 조회다. relation별 점유는 `pg_buffercache`를 `pg_class.relfilenode`와 현재 DB로 묶어
  집계한다. `pg_buffercache_evict*`는 캐시를 비우므로 운영 진단에 쓰지 않는다.
- amcheck는 운영에서 `bt_index_check`만 쓴다. `bt_index_parent_check`는 ShareLock으로 쓰기를
  막는다. `heapallindexed => true`는 heap 전체를 읽으므로 한 번에 한 인덱스씩, 부하가 낮을 때
  실행한다. `REINDEX CONCURRENTLY` 전후와 major upgrade 전 검사에 사용한다.
- auto_explain 계획은 PostgreSQL 로그(Loki)에 `duration: ... plan:`으로 남는다. 시간 계측을 끈
  계획이므로 노드별 시간은 없고 실제 행 수와 버퍼만 있다. 운영 계측 비용은 같은 query ID의
  `pg_stat_statements` 구간 평균을 활성화 전후로 비교해 확인한다.
