# PostgreSQL 확장 검토·검증·운영 적용

운영 DB 리뷰에서 확인한 live 상태 조회·보존 처리의 CPU와 대기 원인을 구분하도록
`pg_stat_kcache`와 `pg_wait_sampling` 도입안을 준비한다. `pg_jsonschema`는 작은 JSON
계약에서 기존 검사와 결과·비용을 비교하는 실험 대상으로만 다룬다.

## 범위와 제약

- 기준 revision: `4a633ab24`. 기존 checkout의 Compose·문서 변경을 보존하는 별도 작업트리다.
- 상위 `/home/kapu/work/iris-stack/AGENTS.md`와 `.agents/workflows.md`를 적용한다.
- 빌드·시험은 kapu의 격리된 테스트 컨테이너에서 수행한다. 운영 데이터 복제는 하지 않는다.
- PostgreSQL 18.6·Alpine, locale·checksum·UID, 기존 gosu 교체를 유지한다.
- 초기 범위는 로컬 검증이며, 후속 승인에 따른 운영 적용 결과는 아래에 기록한다. 원격 Git 게시는 포함하지 않는다.
- 운영 반영은 검증된 이미지·설정·활성화 SQL·복구 절차를 준비한 뒤 대상과 효과의 승인을 확인한다.

## 작업 결과

- [x] PG18 호환 공식 source를 SHA256으로 고정하고 빌드 의존성을 최종 이미지에서 분리했다.
- [x] amd64 root/UID 999 기동과 CPU·sleep·행 잠금 대기·권한·재시작을 검증했다.
- [x] kapu BuildKit 에뮬레이션으로 ARM64의 같은 기능을 검증했다. ARM 실기 성능은 미측정이다.
- [x] 24개 합성 부하 측정에서 실패 transaction 0건, 동시 계측의 TPS 감소는 0~3.6%였다.
- [x] `pg_jsonschema`의 동일 aliases 계약은 통과했으나 내장 검사보다 약 23.5배 느려 보류했다.
- [x] [운영 적용·복구](../runbooks/postgres-observability.md)와 [실험 결과](../../../scripts/experiments/postgres-extensions/README.md)를 정리했다.

Compose의 기본 preload는 유지한다. Osaka 운영 호스트는 후속 사용자 승인으로 아래 두 계측
확장을 활성화했다. JSON Schema 실험은 GNU/amd64 환경에 한정하며 운영에 설치하지 않았다.
최종 이미지 검사에서 기존 `nghttp2-libs`의 `CVE-2026-58055` 보고를 확인하여,
같은 Alpine 저장소의 수정 버전 `1.70.0-r0`으로 해당 패키지만 갱신한다.
모의 설치의 변경 집합은 이 패키지 1개이며 검사 예외를 추가하지 않는다.

## 운영 적용 결과 — 2026-10-07

- 사용자 후속 승인에 따라 Osaka `holo-postgres`만 `--no-build --no-deps`로 재생성했다.
  기존 `hololive-bot_holo-pg-data` volume을 유지했다.
- 적용 이미지 revision: `edf6f7efc2230de0667a96d5a0e3d06bccfc5004` (arm64).
  image ID: `sha256:126da0959cac41ae0900615ac348c345a0a959c6282985ed2ccfe8348f33e37b`.
  재시작한 PostgreSQL postmaster 시각: `2026-10-07T07:22:50Z`.
- `pg_stat_kcache` 2.3.2와 `pg_wait_sampling` binary 1.1.11 / SQL extension 1.1 활성화를
  한 transaction으로 완료했다. 기존 pg_stat_statements·pgstattuple은 유지했다.
- 운영 Compose는 해당 PostgreSQL service의 변경분만 적용하여 기존 다른 service의
  logging 변경을 보존했다. 비밀 아닌 preload 키는 stack-secrets 마스터에서 변경하고
  Hololive 범위로 동기화했으며 manifest 25개 경로의 owner/mode를 확인했다.
- 적용 후 중앙 API·worker·collector와 AP `a`/`b`/`d` readiness가 통과했다.
  중앙 앱 container ID는 유지되었고 재시작 횟수는 0이었다.
- `07:27:26Z` 읽기 검사에서 앱 runtime/scraper의 CPU 누적값이 각각 14.81/10.49초였고,
  실제 `DataFileRead`, `AioIoCompletion`, `transactionid`, `BufferContent` 대기 표본을 확인했다.
  전체 profile 48 entries, query ID가 있는 entries 32개였다. 장기 성능 영향의 측정값은 아니다.
- 카탈로그 검사에서 앱의 새 통계 읽기와 exporter의 reset 권한이 없음을 확인했다.
  네트워크 DB 연결 37개는 모두 TLSv1.3이고 plaintext 연결·잠금 대기·1분 이상 transaction은 0이었다.
- 재시작의 `07:22:49~50Z`에 API/중앙 collector의 일시적인 연결 오류가 기록되었다.
  재시작 1분 뒤부터 확인 시점까지 중앙 서비스의 추가 ERROR/FATAL/PANIC 로그는 없었다.
- 복구 이미지 태그: `hololive-postgres:rollback-pg-observability-20261007T071616Z`.
  이전 Compose와 실행 기록은 운영 호스트의
  `/opt/hololive-bot/compose/rollbacks/pg-observability-20261007T071616Z/`에 보존했다.
  운영 적용에는 원격 Git 게시를 포함하지 않았다.

실제 `SET ROLE postgres_exporter` 조회에서 운영의 public USAGE 차단을 발견했다.
함수/테이블 권한만 검사한 초기 확인으로는 이 조건을 잡지 못했다. public 접근을 넓히지 않고
`hololive_observability` 전용 스키마와 숫자 지표 view를 제공하도록 수정한다. 로컬 시험도
public USAGE를 제거한 상태에서 실제 모니터 조회·비활성화를 검증하도록 보강했다.

최종 수정은 revision `af9b2b57cbf2e18b34f9ccecc46b28396e054ba3`의 arm64 이미지
`sha256:4a23abfb0d1ce0427a71b1fdce4210b6ff223e7fad3c4cd801b938137205cbd6`로 적용했다.
Postmaster 시작 시각은 `2026-10-07T07:55:48Z`이다. 두 확장을 DROP하지 않고 한 transaction의
`ALTER EXTENSION ... SET SCHEMA`로 이동했으며, 기존 public 확장이 있는 격리 DB에서 이
이동과 모니터 조회를 먼저 검증했다. 수정 이미지의 amd64/ARM64 기능 시험과 최신 취약점 DB
스캔도 통과했다.

`07:57:12Z`에 실제 `SET ROLE postgres_exporter`로 이미지의 읽기 SQL 전체를 통과했다.
exporter의 public USAGE는 계속 false, 전용 스키마 USAGE는 true이며 CREATE/reset은 false다.
앱의 전용 스키마 USAGE도 false다. kcache 누적값의 재시작 복원과 wait 표본의 새 수집을
확인했고, 당시 네트워크 연결 33개 모두 TLSv1.3이었다. 최종 이미지·같은 데이터 volume·중앙 및
전체 AP readiness를 대조했으며, 최종 재시작 1분 뒤부터 확인 구간의 중앙 추가 오류는 0건이다.
기존 앱 container ID와 native AP 프로세스의 기동 상태는 유지했다. 최종 기록은 같은 복구
디렉터리의 `schema-correction-result.json`, `final-containers.json`, `post-statistics.txt`에 남겼다.

## 서울 이전과 로컬 main 통합 — 2026-10-07

위 Osaka 기록 이후 중앙 DB를 `hololive-seoul`로 이전했습니다. 서울이 기존 중앙 주소
`100.100.1.8`을 승계했으며 같은 최종 PostgreSQL image revision과 확장 버전·preload·전용
스키마·권한을 유지합니다. `postgres_exporter` 역할의 실제 읽기와 앱 접근·reset 권한
차단을 서울에서 다시 확인했습니다. Osaka VM은 삭제하고 이전 시점의 100GB 부트 볼륨을
분리 보존하므로, 위 복구 디렉터리는 현재 운영 서버 경로가 아닙니다.

`codex/postgres-observability`의 구현·검증·운영 기록을 로컬 main에 통합했습니다.
기존 서울 이전 문서와 PO issuer 로그 회전 수정을 보존했으며, 활성화·복구 runbook의
대상과 secret master 경로를 서울로 변경했습니다. 이번 통합은 원격 Git 게시·운영 재배포·
DB 재시작·백업 재개를 포함하지 않습니다.

통합 후 `scripts/ci/test-infra-images.sh`를 kapu에서 통과했습니다. 격리된 amd64 PostgreSQL을
root·UID 999로 각각 실행하여 preload 누락 거부, 활성화 재적용, CPU·행 잠금 대기 수집,
조회 권한 경계, 재시작과 비활성화를 확인했습니다. 스택 DB 영속 볼륨 계약 검사도 통과했습니다.
통합 Compose의 PG 실행 인자는 서울 운영본과 일치하고, 계측 SQL 세 파일의 SHA-256도
실행 이미지와 일치합니다. PO issuer의 5 MiB × 3 로그 회전을 보존했습니다. 이번 통합에서
ARM64를 다시 빌드·실행하지 않았으며, 앞선 ARM64 검증 기록과 변경 없는 운영 이미지를 유지합니다.

## 검증

기존 인프라 이미지 시험의 root/UID 999 시작과 SQL 검사를 유지한다. 추가 검증은 실제
확장 로드·쿼리 통계·대기 표본·권한·재시작 동작을 대상으로 한다. 성능 비교는 같은
이미지에서 계측 비활성/활성을 번갈아 실행하고 절대 성능과 상대 비용을 함께 기록한다.
ARM64 실행이 지원되지 않으면 빌드 성공과 실행 검증을 구분해 보고한다.

Fallback delta: 새 대체 실행 경로나 오류 무시를 추가하지 않는다.
