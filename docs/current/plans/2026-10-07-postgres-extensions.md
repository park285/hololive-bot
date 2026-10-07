# PostgreSQL 확장 검토와 로컬 검증

운영 DB 리뷰에서 확인한 live 상태 조회·보존 처리의 CPU와 대기 원인을 구분하도록
`pg_stat_kcache`와 `pg_wait_sampling` 도입안을 준비한다. `pg_jsonschema`는 작은 JSON
계약에서 기존 검사와 결과·비용을 비교하는 실험 대상으로만 다룬다.

## 범위와 제약

- 기준 revision: `4a633ab24`. 기존 checkout의 Compose·문서 변경을 보존하는 별도 작업트리다.
- 상위 `/home/kapu/work/iris-stack/AGENTS.md`와 `.agents/workflows.md`를 적용한다.
- 빌드·시험은 kapu의 격리된 테스트 컨테이너에서 수행한다. 운영 데이터 복제는 하지 않는다.
- PostgreSQL 18.6·Alpine, locale·checksum·UID, 기존 gosu 교체를 유지한다.
- 운영 DB 쓰기·컨테이너 교체·재시작·원격 Git 게시는 이번 로컬 검증에 포함하지 않는다.
- 운영 반영은 검증된 이미지·설정·활성화 SQL·복구 절차를 준비한 뒤 대상과 효과의 승인을 확인한다.

## 작업 결과

- [x] PG18 호환 공식 source를 SHA256으로 고정하고 빌드 의존성을 최종 이미지에서 분리했다.
- [x] amd64 root/UID 999 기동과 CPU·sleep·행 잠금 대기·권한·재시작을 검증했다.
- [x] kapu BuildKit 에뮬레이션으로 ARM64의 같은 기능을 검증했다. ARM 실기 성능은 미측정이다.
- [x] 24개 합성 부하 측정에서 실패 transaction 0건, 동시 계측의 TPS 감소는 0~3.6%였다.
- [x] `pg_jsonschema`의 동일 aliases 계약은 통과했으나 내장 검사보다 약 23.5배 느려 보류했다.
- [x] [운영 적용·복구](../runbooks/postgres-observability.md)와 [실험 결과](../../../scripts/experiments/postgres-extensions/README.md)를 정리했다.

운영 기본 preload는 유지한다. 이번 작업의 활성화 대상은 두 계측 확장뿐이며 운영 적용은
별도 승인 전까지 실행하지 않는다. JSON Schema 실험은 GNU/amd64 환경에 한정한다.
최종 이미지 검사에서 기존 `nghttp2-libs`의 `CVE-2026-58055` 보고를 확인하여,
같은 Alpine 저장소의 수정 버전 `1.70.0-r0`으로 해당 패키지만 갱신한다.
모의 설치의 변경 집합은 이 패키지 1개이며 검사 예외를 추가하지 않는다.

## 검증

기존 인프라 이미지 시험의 root/UID 999 시작과 SQL 검사를 유지한다. 추가 검증은 실제
확장 로드·쿼리 통계·대기 표본·권한·재시작 동작을 대상으로 한다. 성능 비교는 같은
이미지에서 계측 비활성/활성을 번갈아 실행하고 절대 성능과 상대 비용을 함께 기록한다.
ARM64 실행이 지원되지 않으면 빌드 성공과 실행 검증을 구분해 보고한다.

Fallback delta: 새 대체 실행 경로나 오류 무시를 추가하지 않는다.
