# PGO 운영 계약

## 현재 정책

`hololive-api`와 `hololive-alarm-worker`의 기본 빌드는 PGO를 사용하지 않습니다.
`scripts/perf/pgo/default-policy.tsv`는 `off` 행만 허용하고
`scripts/ci/check-pgo-default.sh`는 `on` 행을 즉시 거부합니다.

`hololive-api`와 `hololive-alarm-worker` Dockerfile의 모든 Go build는 `-pgo=off`를
명시합니다. Compose에는 `GO_PGO_FILE` build arg가 없고 환경 변수 override도 제공하지
않습니다. 게이트는 Dockerfile과 `docker compose config --no-interpolate --format json`
결과를 구조적으로 검사하여 다음을
차단합니다.

- `default.pgo`, `.meta.json`, `.hotpaths` artifact가 남거나 새로 추가된 경우
- 기본 Go build 결과에 `-pgo` stamp가 있는 경우
- policy에 `on` 또는 알 수 없는 mode가 있는 경우
- 관리 대상 Dockerfile이 `ARG GO_PGO_FILE`을 노출하거나 Go build에서 `-pgo=off`를 빠뜨린 경우
- Compose service가 `GO_PGO_FILE` build arg를 갖는 경우

이 정책은 2026-07-10 기준으로 두 서비스에 적용되던 짧고 출처가 불완전한 profile을
제거하면서 도입했습니다. 운영 rejection metric과 재검증 가능한 collection binary가 없는
기존 profile은 기본 빌드 근거로 사용할 수 없습니다.

## 연구용 도구

`scripts/perf/pgo/generate.sh`와 `compare-pgo.sh`는 연구 candidate와 비교 자료를 만드는
도구입니다. 이 도구의 `ACCEPTED` verdict나 metadata는 default PGO 채택 권한을 부여하지
않으며 `default-policy.tsv`를 `on`으로 바꿀 수 없습니다.

`generate.sh validate-meta`는 연구 자료의 최소 무결성만 확인합니다.

- `acceptedBy`는 정확히 `scripts/perf/pgo/generate.sh`
- `generatedAt`과 pprof `Time:`에서 추출한 `profileCollectedAt`은 timezone-aware ISO-8601이며
  profile hash/build ID와 함께 검증됨
- `expiresAfterDays`는 양의 정수이며 최대 45
- 비교 metric은 유한한 숫자이고 CPU/p95 +3%, p99 +0%, RSS +3%, binary +5%,
  hot benchmark -3% 상한을 포함한 연구 verdict를 다시 계산해 `ACCEPTED`
- profile SHA-256, pprof build ID, main import path, 실제 duration 계약이 일치

`collectionBinarySha256`는 생성 시 사용한 일시적 binary의 해시를 기록한 연구 evidence일
뿐입니다. binary 자체가 보존되지 않으므로 이후 검증 가능한 attestation 또는 채택 근거로
간주하지 않습니다.

## 재도입 차단

현재 release gate에는 PGO default-on 경로가 없습니다. 재도입하려면 먼저 별도 설계 변경으로
다음 중 하나를 구현하고 독립 리뷰를 통과해야 합니다.

1. profile을 수집한 정확한 binary를 보존하고 hash, ELF build ID, Go main package,
   source revision을 release gate에서 다시 검증하는 방식
2. 위 항목과 workload/성능 결과를 포함하는 서명된 동등 attestation을 검증하는 방식

그 설계에는 대표 workload, 600초 이상 profile, p99/RSS/binary/hot benchmark rejection
기준, freshness/rollback 계약도 포함해야 합니다. 설계와 검증이 들어오기 전에는 profile이나
metadata를 추가하거나 Compose override를 복원하지 않습니다.

라이브 트래픽, secret, 배포 또는 재시작이 필요한 연구 collector는 별도 승인을 받은 뒤
실행합니다. 현재 변경은 다음 image rebuild부터 PGO를 제거하며 실행 중인 container에는
영향을 주지 않습니다.

## 검증

```bash
bash scripts/ci/check-pgo-default_test.sh
bash scripts/ci/check-pgo-default.sh
bash scripts/ci/check-pgo-freshness_test.sh
bash scripts/ci/check-pgo-freshness.sh --strict
bash scripts/perf/pgo/compare_test.sh
bash scripts/perf/pgo/compare_regression_test.sh
bash scripts/perf/pgo/generate_test.sh
```
