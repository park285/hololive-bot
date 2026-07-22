# PGO 운영 계약

## 현재 정책

`hololive-api`와 `hololive-alarm-worker`의 production build는 PGO를 사용하지 않습니다.
`scripts/ci/pgo-off-policy.tsv`는 두 서비스의 `off` 행만 소유하며,
`scripts/ci/check-pgo-default.sh`는 `on` 행이나 알 수 없는 mode를 즉시 거부합니다.

`hololive-api`와 `hololive-alarm-worker` Dockerfile의 모든 Go build는 `-pgo=off`를
명시합니다. Compose에는 `GO_PGO_FILE` build arg가 없고 환경 변수 override도 제공하지
않습니다. 게이트는 Dockerfile과 `docker compose config --no-interpolate --format json`
결과를 구조적으로 검사하여 다음을 차단합니다.

- `default.pgo`, `.meta.json`, `.hotpaths` artifact가 남거나 새로 추가된 경우
- 기본 Go build 결과에 `-pgo` stamp가 있는 경우
- policy에 `on` 또는 알 수 없는 mode가 있는 경우
- 관리 대상 Dockerfile이 `ARG GO_PGO_FILE`을 노출하거나 Go build에서 `-pgo=off`를 빠뜨린 경우
- Compose service가 `GO_PGO_FILE` build arg를 갖는 경우

이 정책은 2026-07-10 기준으로 두 서비스에 적용되던 짧고 출처가 불완전한 profile을
제거하면서 도입했습니다. 운영 rejection metric과 재검증 가능한 collection binary가 없는
기존 profile은 production build 근거로 사용할 수 없습니다.

## 수동 성능 조사 경계

Go benchmark 함수는 필요할 때 표준 `go test -bench` 명령으로 직접 실행합니다. 이 결과는
특정 조사 환경의 profiling 자료이며 release 또는 PGO 채택 verdict가 아닙니다. 머신별
wall-clock baseline, profile generator, 숫자 비교, freshness, approved workload 자동화는
blocking gate가 아닙니다.

## 향후 PGO 도입

현재 release gate에는 PGO default-on 경로가 없습니다. 향후 도입은 별도 설계 변경으로
대표 workload, 수집 binary와 source revision의 재검증 가능한 provenance, 성능·용량·tail
latency·rollback 기준, profile 수명 및 운영 관측 경로를 명시하고 독립 승인을 받아야 합니다.
그 설계와 evidence path가 승인되기 전에는 profile 또는 metadata를 추가하거나
`GO_PGO_FILE`·implicit auto PGO 경로를 복원하지 않습니다.

라이브 트래픽, secret, 배포 또는 재시작이 필요한 조사는 별도 승인을 받은 뒤 실행합니다.

## 검증

```bash
bash scripts/ci/check-pgo-default_test.sh
bash scripts/ci/check-pgo-default.sh
```
