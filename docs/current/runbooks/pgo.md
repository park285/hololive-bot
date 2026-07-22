# PGO 운영 계약

## 현재 정책

`hololive-api`와 `hololive-alarm-worker`의 기본 빌드는 PGO를 사용하지 않습니다.
`scripts/ci/pgo-off-policy.tsv`는 production PGO 정책의 정본이며 `off` 행만 허용하고
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

## 재도입 경계

현재 release gate에는 PGO default-on 경로가 없습니다. 향후 PGO를 도입하려면 대표 workload,
profile provenance, 재현 가능한 성능·자원 rejection 기준, rollout/rollback, observability를
소유하는 별도 설계와 명시적 승인이 필요합니다. 그 설계가 채택되기 전에는 profile이나
metadata를 추가하거나 Compose override를 복원하지 않습니다.

라이브 트래픽, secret, 배포 또는 재시작이 필요한 성능 조사는 별도 승인을 받은 뒤 실행합니다.

## 검증

```bash
bash scripts/ci/check-pgo-default_test.sh
bash scripts/ci/check-pgo-default.sh
```
