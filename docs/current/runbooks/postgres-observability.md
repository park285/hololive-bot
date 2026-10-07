# PostgreSQL optional observability

## 범위

`hololive-osaka` (`100.100.1.8`)의 `holo-postgres`/`hololive`에
`pg_stat_kcache` 2.3.2와 `pg_wait_sampling` binary 1.1.11(SQL extension 1.1)을
선택적으로 활성화하는 절차다. 기본 preload는 계속 `pg_stat_statements` 하나다.
Dockerfile은 고정 PostgreSQL 18.6/Alpine PGXS로 두 CPU 아키텍처의 모듈을 빌드하며
앱 테이블·마이그레이션·initdb를 변경하지 않는다.

운영 이미지 전송·설정 쓰기·재시작·CREATE/DROP EXTENSION은 명시적으로 승인된 뒤
`hololive-bot-ops`의 현재 no-build 배포 절차로 수행한다. 원격 Git 게시는 별도 범위다.
기존 volume에 initdb를 재실행하지 않는다. `postgres-replication.md`의 single-primary
경계와 현재 `DEPLOYMENT_BASELINE.md`를 유지한다.

로컬 결과와 재현 명령은 [extension experiments](../../../scripts/experiments/postgres-extensions/README.md)에 있다.
계측은 DB 안의 쿼리 CPU·대기를 설명하며 Go의 pool 대기·decode·GC 시간을 직접 측정하지 않는다.

## 로컬 준비

깨끗하고 검토된 작업트리의 전체 SHA를 `REVISION`에 넣는다. 컴파일·시험은 kapu에서만 한다.

```bash
bash scripts/ci/test-infra-images.sh
docker buildx build --platform linux/arm64 --target test \
  --build-arg REVISION="$(git rev-parse HEAD)" --output type=cacheonly deploy/images/postgres
docker buildx build --platform linux/arm64 --target runtime \
  --build-arg REVISION="$(git rev-parse HEAD)" --provenance=false --sbom=false \
  --load --tag hololive-postgres:observability-candidate deploy/images/postgres
docker image inspect --format '{{.Id}} arch={{.Architecture}} revision={{index .Config.Labels "org.opencontainers.image.revision"}}' \
  hololive-postgres:observability-candidate
```

root/UID 999 시작, preload 누락 거부, CPU 계측, sleep/행 잠금 표본, 권한, 재시작,
SQL 비활성화 뒤 기본 preload 복귀를 검증한다. ARM64 에뮬레이션의 `linux_hz` 자동 감지값을
운영 설정으로 복사하지 않는다. 최종 이미지에는 compiler·테스트 데이터가 들어가지 않는다.
저장소의 최종 이미지 스캔을 후보 태그만 담은 별도 manifest로 실행한다.

## 활성화

1. 운영 `transaction_read_only=on` 증명 뒤 현재 확장·preload·query ID·긴 transaction·잠금,
   readiness와 동일 구간 DB 지표를 확인한다. 기존 image ID/tag와 deploy tree, env의 해당
   비밀 아닌 설정 값을 복구점으로 기록한다. env 전체나 credentials를 출력하지 않는다.
2. 검증한 arm64 이미지와 필요한 배포 파일만 전송한다. 새 image의 architecture·전체 SHA와
   Compose config를 대조한다. 기존 이미지와 volume은 보존한다.
3. 호스트 `/etc/stack-secrets/hololive-bot/compose.env`의 아래 비밀 아닌 키만 승인 범위에서
   갱신한다. 수동 명령과 systemd 재시작 모두 같은 값을 읽도록 한다.

   ```dotenv
   HOLOLIVE_POSTGRES_PRELOAD_LIBRARIES=pg_stat_statements,pg_stat_kcache,pg_wait_sampling
   ```

   Compose는 kcache `track=top`, planning off, wait `profile_pid=off`,
   `profile_queries=top`, `sample_cpu=off`, profile 20ms/history 100ms·20,000 entries를 사용한다.
   기본 비활성 상태의 custom GUC placeholder와 활성 상태 모두 로컬 기동을 검증한다.
4. 승인된 새 이미지를 선택한 현재 prod+live-compat 파일 세트로 `config --quiet` 후
   `up -d --no-build --no-deps holo-postgres`만 실행한다. DB 연결이 잠시 끊기고 진행 중인
   transaction은 중단될 수 있다. 앱의 재연결과 작업 재시도를 확인한다. AP·앱 이미지는 바꾸지 않는다.
5. 정상 시작 뒤 관리자 socket으로 다음 파일을 실행한다. 기존 volume에도 직접 실행해야 한다.

   ```bash
   sudo docker exec holo-postgres psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 \
     -f /usr/local/share/hololive/enable-observability.sql
   ```

   한 transaction에서 preload/version/schema를 검증하고 확장을 생성한다. 새 통계 읽기는
   `pg_read_all_stats` 구성원에게만 부여하며 reset은 관리자만 가능하다. 기존 앱 역할을
   확장하지 않는다. 2026-10-07 읽기 확인에서는 `postgres_exporter`, `iris_db_nightly`가
   이미 이 통계 역할을 상속했다. 적용 직전에 재확인하고 새 역할 GRANT를 추정해서 하지 않는다.
6. 실제 running image ID, extension version, preload, CPU/wait 수집, 권한을 확인한다.
   central/API/worker/collector와 각 AP readiness, DB 연결 오류·새 worker crash/OOM을 확인한다.
   장애가 지속되면 승인된 복구 범위로 되돌린다.

## 관측

관리자 또는 기존 통계 역할로 두 번의 제한된 snapshot을 수집한다.

```bash
sudo docker exec -e PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=1000' \
  holo-postgres psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/read-observability.sql
```

- query text·payload는 출력하지 않는다. query ID별 누적 CPU·실행 시간·calls·bytes의
  같은 구간 차분을 비교한다. 기존 pg_stat_statements는 오래된 누적값이므로 신규 kcache의
  절대 누적값과 바로 비율을 계산하지 않는다. 재시작/reset/음수 차분은 새 구간으로 취급한다.
- kcache의 filesystem bytes는 PostgreSQL logical buffers와 다르다. background I/O worker와
  checkpoint 등 서버 전체 I/O를 호출 쿼리에 완전히 귀속하는 지표가 아니다.
- wait profile은 cluster 범위이며 dbid/userid가 없다. query ID만으로 다른 DB·role의
  pg_stat_statements와 join하면 중복/오귀속될 수 있다. 표본 수×20ms는 대략적 누적 대기이며
  정확한 latency나 CPU 시간과의 항등식을 뜻하지 않는다. 짧은 대기는 놓칠 수 있다.
- kcache는 정상 재시작에서 저장된 통계를 복원한다. wait history/profile은 메모리 통계로
  재시작에서 소실된다. 20,000 제한은 history ring에만 적용되며 profile cardinality 상한이 아니다.
  `profile_pid=off`로 PID 증가를 막되 query ID 변화에 따른 entry 수·메모리를 관찰한다.
  통계 reset은 읽기 수집에 넣지 않는다. 필요하면 대상/손실 구간 승인을 받은 관리자가 수행한다.

## 복구

새 모듈 파일이 있는 이미지가 아직 실행 중일 때 아래 SQL로 두 확장만 제거한다.
이 작업은 새 계측 통계/객체를 버리고 앱 테이블·기존 pg_stat_statements는 보존한다.
`RESTRICT`가 외부 의존 객체 때문에 실패하면 중단하고 그 객체를 검토한다. `CASCADE`로 우회하지 않는다.

```bash
sudo docker exec holo-postgres psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/disable-observability.sql
```

해당 env 키를 종전 값(이번 도입 전에는 미설정/`pg_stat_statements`)으로 복구하고 이전
image ID/tag와 deploy tree로 같은 `--no-build --no-deps holo-postgres` 재생성을 수행한다.
같은 PGDATA volume을 유지하고 readiness·앱 재연결·기존 통계를 확인한다.
새 preload를 남긴 채 모듈 없는 이전 이미지로 바꾸면 시작에 실패한다.

새 preload 때문에 서버가 시작하지 못하면, 먼저 **새 모듈 파일을 가진 이미지**를 유지한 채
preload만 `pg_stat_statements`로 복구하여 시작하고 위 DROP SQL을 실행한 다음 이전 이미지로
복귀한다. 모듈/확장이 생성되지 않았어도 `IF EXISTS` 경로를 쓸 수 있다.
volume 삭제·초기화·앱 데이터 restore는 이 복구 절차에 포함하지 않는다.

Fallback delta: 자동 fallback이나 오류 무시 경로를 추가하지 않는다.
