# Proposed script: check-internal-route-hardcoding.sh

목적: internal route path가 contracts package 밖에 하드코딩되는 것을 방지합니다.

검사 대상 route prefix:

- `/internal/trigger`
- `/internal/membernews`
- `/internal/majorevent`
- `/internal/alarm`

허용 경로:

- `hololive/hololive-shared/pkg/contracts/**`
- `**/*_test.go`
- `docs/current/contracts/**`
- 필요 시 `script-proposals/**`
