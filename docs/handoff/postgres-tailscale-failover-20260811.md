# Handoff: Hololive PostgreSQL Tailscale Service 자동 failover 완결

## Status: LOC SPLIT COMPLETE — AWAITING PUSH APPROVAL

기능 구현, NFR/security 검토, LOC 분리가 모두 완료됐습니다. 세 파일 모두 threshold 안에 들어왔고
targeted validation은 재실행해 통과했습니다. full pre-push, 원격 branch, PR, main merge, live rollout은
아직 시작되지 않았으며 사용자의 명시 승인을 기다리는 상태입니다.

## Task identity

- Task ID: `HB-DBHA-TAILSCALE-20260811`
- 역할: continue, validate, amend, publish, merge, live rollout
- worktree: `/home/kapu/work/iris-stack/hololive-bot`
- branch: `feat/postgres-tailscale-service-failover`
- feature commit: LOC 분리를 흡수해 `6fb0d661a`에서 amend된 이 branch의 단일 commit
- base/current `origin/main`: `51c0244a87ffee013a7f36ecff96c8b15ac6693b`
- snapshot date: `2026-08-11 KST`

이 문서는 작업 상태와 안전 경계를 전달하는 증거입니다. 다른 세션의 remote write, secret write,
production deploy 권한을 자체적으로 부여하지 않습니다. 새 세션은 사용자의 현재 명시 승인을 확인한
뒤 push, PR, merge, stack-secrets 변경, live restart/apply를 수행해야 합니다.

## Repository state at handoff

feature commit은 로컬에만 존재하며 LOC 분리 결과를 모두 흡수했습니다.

- remote branch `feat/postgres-tailscale-service-failover`: 없음
- open GitHub PR: 없음
- `main`과 `origin/main`: `51c0244a8`로 일치
- working tree: clean (LOC 분리 산출물은 amend로 흡수됨)

amend에 포함된 LOC 분리 산출물입니다.

```text
M  scripts/deploy/test-compose-services.sh
M  scripts/ops/integration/postgres-failover-integration_test.sh
M  scripts/ops/lib/postgres-failover-test-cases-transition.sh
M  scripts/ops/postgres-failover_test.sh
A  docs/handoff/postgres-tailscale-failover-20260811.md
A  scripts/deploy/test-compose-postgres-routing.sh
A  scripts/ops/integration/lib/postgres-failover-integration-lib.sh
A  scripts/ops/lib/postgres-failover-test-cases-primary.sh
A  scripts/ops/lib/postgres-failover-test-cases-unfence.sh
```

meta-repo `/home/kapu/work/iris-stack`는 `main` `da074809d`이며 다음 user-owned dirty state가
있습니다. Hololive gitlink 갱신 때 이 변경을 건드리거나 함께 stage하지 마십시오.

```text
 m Iris
 M docs/agent-workflows/plans/2026-08-08-hololive-central-vm-migration.md
 M docs/agent-workflows/plans/2026-08-08-openbao-decommission-stack-wide.md
 M hololive-bot
 m iris-bridge
```

## Implemented outcome

LOC 분리를 포함한 `origin/main` 대비 최종 diff는 37개 파일 규모입니다.
(LOC 분리 이전 기능 commit 단독 기준은 32개 파일, 2,624 insertions, 106 deletions이었습니다.)

- stable endpoint: `svc:hololive-postgres`
- service DNS: `hololive-postgres.tail742dd8.ts.net:5433`
- old primary backend: `100.100.1.8:5433`
- standby/new primary backend: `100.100.1.5:5434`
- PostgreSQL runtime/test image: `18.4`; PG16이 아님
- central/AP의 모든 DB consumer에 Tailscale DNS `100.100.100.100`을 첫 resolver로 배치
- Compose와 Osaka/Osaka2 native producer가 동일 stable PostgreSQL endpoint를 사용
- fence는 전체 Compose가 아니라 `deunhealth`와 `holo-postgres`만 정지
- 첫 외부 변경 전에 durable `fence.intent` 기록
- restart-policy 변경과 Service drain 직후 SIGKILL crash injection coverage
- promotion 후 route 재시도는 재승격 없이 idempotent하게 수행
- unfence는 old primary를 새 primary의 streaming read-only standby로 재구성·검증한 뒤에만 허용
- 실제 PostgreSQL 18.4 2-node isolated apply harness 추가

사용자가 제기한 “왜 전부 root인가” 문제는 다음 경계로 해결했습니다.

- controller: 기존 `hololive-pg-failover`
- primary SSH: `hololive-pg-fence`
- route SSH: `hololive-pg-route`
- 두 SSH 계정: root-owned `/var/empty`, `/bin/dash`
- root-owned `AuthorizedKeysFile`, 계정별 `ForceCommand`, `DisableForwarding`
- dispatcher: `#!/bin/dash`, exact argv/account validation
- root 작업은 고정 sudo helper만 허용
- arbitrary, cross-role, multiline, `.bashrc`, `BASH_ENV`, `ENV` negative coverage

## Review result

최종 NFR 및 privilege-boundary 재검토는 모두 PASS입니다.

- `final_nfr_gate_tailscale`: `NFR_READY`, Critical/Important 없음
- `route_security_review`: PASS, Critical/Important 없음
- `endpoint_docs_review`: PASS, Critical/Important 없음

검토 중 발견해 이미 닫은 결함:

1. `advertised:false`를 jq `// true`가 통과시키던 route 검증 오류
2. route/restart 변경보다 늦게 intent를 쓰던 host-crash window
3. `restrict`만 사용해 SSH arbitrary command가 가능하던 경계
4. `/bin/bash`와 user-owned home을 통한 pre-`ForceCommand` startup escape

## Fresh validation already passed

기능 commit 상태와 이후 security 수정 상태에서 다음 증거가 있습니다.

- non-root `bash scripts/ops/postgres-failover_test.sh`: PASS
- root `sudo -n bash scripts/ops/postgres-failover_test.sh`: PASS
- `bash scripts/ops/postgres-failover-ssh-dispatch_test.sh`: PASS
- `bash scripts/deploy/test-compose-services.sh`: PASS
- `bash scripts/deploy/ap-host-native-deploy_test.sh`: PASS
- Compose H3/security contract tests: PASS
- touched production shell `bash -n`/`shellcheck`: PASS
- both sudoers `visudo -cf`: PASS
- both sysusers `systemd-sysusers --dry-run`: PASS
- Go function budget: PASS
- `git diff --check`: LOC split 전 commit 상태에서 PASS

실제 isolated PostgreSQL 18.4 harness도 최종 기능 상태에서 PASS했습니다.

- TLS `verify-full`
- physical replication slot
- `pg_basebackup`
- streaming zero lag
- least-privilege promotion role
- old primary non-writable after fence
- new primary exact `f|off`
- complete promotion/route marker
- idempotent route re-probe
- Docker container/volume/network residue 0

명령:

```bash
HOLOLIVE_POSTGRES_FAILOVER_INTEGRATION=1 \
  bash scripts/ops/integration/postgres-failover-integration_test.sh
```

## Pre-push LOC failure (resolved)

최초 실행한 command:

```bash
FULL_PRE_PUSH=true git push -u origin feat/postgres-tailscale-service-failover
```

push는 원격 ref를 만들기 전에 다음 gate에서 중단됐습니다.

```text
FAIL: file LOC threshold violations detected
 - missing-threshold:scripts/ops/integration/postgres-failover-integration_test.sh:506>400
 - missing-threshold:scripts/ops/lib/postgres-failover-test-cases-transition.sh:588>400
 - missing-threshold:scripts/deploy/test-compose-services.sh:415>400
```

threshold 예외는 추가하지 않았습니다. 구조 분리를 선택했고 세 건 모두 해소했습니다.
LOC gate 이전의 workflow policy, security shell regression, architecture, Compose, failover tests는
통과했지만, 이후 full Go/race/NilAway, static analysis, govulncheck 단계는 아직 실행되지 않았습니다.
full pre-push를 끝까지 통과했다고 주장하면 안 됩니다.

## Compose security contract 갱신

LOC gate를 넘긴 뒤 full pre-push의 Go test 단계에서 `TestRepoComposeProdHardenedDefaults`가
실패했습니다. LOC gate 이전에는 이 단계까지 도달한 적이 없어 이번에 처음 드러난 결함입니다.

`hololive/hololive-shared/pkg/config/settings/repo_security_contract_test.go`가
`POSTGRES_HOST: holo-postgres`와 `POSTGRES_PORT: "5432"`를 리터럴로 검사했는데, 이 feature가
두 값을 `HOLOLIVE_CENTRAL_POSTGRES_HOST`/`HOLOLIVE_CENTRAL_POSTGRES_PORT` override로
파라미터화했습니다. 계약을 리터럴 host가 아니라 override 변수와 안전 기본값 기준으로 갱신했습니다.

- `POSTGRES_HOST: ${HOLOLIVE_CENTRAL_POSTGRES_HOST:-holo-postgres}` 정확히 1회
- `  POSTGRES_PORT: "${HOLOLIVE_CENTRAL_POSTGRES_PORT:-5432}"` 존재

계약의 실질은 약화되지 않았습니다. override 진입점이 하나뿐이라는 점과 기본값이 in-network
container라는 점을 그대로 강제하고, rendered compose 기준 단언(`composeEnvironment`)이
`holo-postgres`/`5432`/`verify-full`로 해석되는지 독립적으로 검증합니다. override 경로 자체는
`scripts/deploy/test-compose-postgres-routing.sh`가 담당합니다.

## LOC split state

### Completed: Compose routing tests

- `scripts/deploy/test-compose-services.sh`: 303 LOC
- new `scripts/deploy/test-compose-postgres-routing.sh`: 127 LOC, mode 0755
- parent가 새 routing test를 실행
- `bash -n`, `shellcheck`, standalone/parent test, diff-check PASS

### Completed: isolated PostgreSQL integration helpers

- `scripts/ops/integration/postgres-failover-integration_test.sh`: 375 LOC
- new `scripts/ops/integration/lib/postgres-failover-integration-lib.sh`: 137 LOC
- `SCRIPT_DIR` 기준 source
- `bash -n`, `shellcheck -x`, `--help`, actual PostgreSQL 18.4 harness PASS
- Docker residue 0

### Completed: primary/unfence fake tests

`primary_*` fake test 파일을 fence 단위와 unfence 단위로 분리했습니다.

- `scripts/ops/lib/postgres-failover-test-cases-transition.sh`: 75 LOC (EOF blank 1줄 제거)
- `scripts/ops/lib/postgres-failover-test-cases-primary.sh`: 301 LOC, mode 0644
- new `scripts/ops/lib/postgres-failover-test-cases-unfence.sh`: 130 LOC, mode 0644
- 이동 대상: `setup_primary_unfence_fake_tools`, `run_primary_unfence`,
  `primary_unfence_requires_reseeded_streaming_standby`
- `PRIMARY_FENCE`/`PRIMARY_UNFENCE` 상수와 두 상수를 모두 쓰는
  `fence_and_unfence_share_transition_lock`은 primary 파일에 남겨 단일 소유자를 유지
- `scripts/ops/postgres-failover_test.sh`가 primary 다음에 unfence를 source (28 LOC)
- runner의 test case list와 함수 이름은 변경 없음

세 lib 파일 모두 plain `shellcheck` clean입니다. runner의 `SC1091`은 source 미추적 info로,
분리 이전과 동일한 클래스이며 `shellcheck -x`를 script 디렉터리에서 실행하면 사라집니다.

### 분리 과정에서 닫은 결함

독립 review로 `6fb0d661a` 대비 fake tool differential probe를 돌려 divergence 5건을 찾았고,
그중 실제 결함 1건을 닫았습니다.

- `crash_fence_process`를 외부 `bin/fence-crash`로 통합하면서, tailscale fake만 injector 실패를
  삼키고 drain을 성공으로 반환했습니다. baseline은 fake 내부 함수의 `exit 97`이 fake 자체를
  종료시켰습니다. `exec`로 종료코드 전파를 복원해 docker `update` 경로와 동일하게 맞췄습니다.
  probe 결과 두 경로 모두 `97`입니다.
- 새 `scripts/ops/integration/lib/postgres-failover-integration-lib.sh`에 `scripts/ops/lib/*`와
  동일한 source-only guard를 추가했습니다.

남긴 divergence: unfence fake의 `-f` 없는 `docker inspect`가 baseline의 `2` 대신 `0`을 돌려줍니다.
현 호출부는 전부 `-f`라 미노출이고 동일 commit의 fence fake도 같은 형태이므로, 여기만 엄격하게
바꾸면 두 fake 사이에 새 비대칭이 생깁니다. `--format` 표기로 바뀌는 시점에 함께 정리하십시오.

## Fresh validation after the LOC split

`2026-08-11` 재실행 결과입니다. 모두 exit 0입니다.

- `bash -n` (transition/primary/unfence/runner): PASS
- `shellcheck` (transition/primary/unfence): PASS, finding 0
- `shellcheck -x postgres-failover_test.sh` (scripts/ops 기준): PASS
- `bash scripts/ops/postgres-failover_test.sh`: PASS
- `sudo -n bash scripts/ops/postgres-failover_test.sh`: PASS
- `bash scripts/deploy/test-compose-services.sh`: PASS
- `bash scripts/deploy/test-compose-postgres-routing.sh` (standalone): PASS
- `bash scripts/ops/postgres-failover-ssh-dispatch_test.sh`: PASS
- `bash scripts/architecture/check-file-loc.sh`: PASS, violation 0
- `git diff --check HEAD`: PASS
- isolated PostgreSQL 18.4 harness: 12 PASS / 0 FAIL, Docker container/volume/network residue 0

harness 실행 환경 주의: 이 workstation shell의 기본 `umask`가 `002`라서 fixture가 만든 temp
디렉터리가 group-writable이 되고 controller의 trusted-path guard가 fail-closed로 막습니다.
failover test는 `umask 022`에서 실행해야 합니다. guard 자체가 정상 동작한 결과이므로 코드 결함이
아닙니다.

## Immediate continuation checklist

1. full pre-push를 실행하고 끝까지 통과한 exit 0을 확보합니다. bypass 금지입니다.

```bash
FULL_PRE_PUSH=true git push -u origin feat/postgres-tailscale-service-failover
```

2. Korean PR을 열고 public fast gate를 기다린 뒤 main에 merge합니다. push 시점의 열린 PR도 다시
   조회하십시오. 이 snapshot에서는 open PR이 0개입니다.
3. local `main`을 `origin/main`에 fast-forward하고 feature branch를 정리합니다.
4. meta-repo에서는 다른 dirty path를 보존한 채 `hololive-bot` gitlink만 별도 stage/commit/push합니다.

amend에 사용한 stage 목록입니다.

```bash
git add -- \
  docs/handoff/postgres-tailscale-failover-20260811.md \
  scripts/deploy/test-compose-services.sh \
  scripts/deploy/test-compose-postgres-routing.sh \
  scripts/ops/integration/postgres-failover-integration_test.sh \
  scripts/ops/integration/lib/postgres-failover-integration-lib.sh \
  scripts/ops/lib/postgres-failover-test-cases-transition.sh \
  scripts/ops/lib/postgres-failover-test-cases-primary.sh \
  scripts/ops/lib/postgres-failover-test-cases-unfence.sh \
  scripts/ops/postgres-failover_test.sh
IRIS_STACK_ALLOW_BULK_STAGE=1 git commit --amend --no-edit
```

## Suggested PR text

Title:

```text
feat(db-ha): Tailscale Service 기반 자동 승격 완결
```

Body 핵심:

- stable Tailscale Service endpoint와 central/AP consumer DNS 정합화
- fail-closed fence/promotion/route/recovery와 dedicated non-root SSH accounts
- PostgreSQL 18.4 actual isolated apply harness
- SIGKILL crash recovery, arbitrary command, route idempotency regression coverage
- production apply는 Service approval, SAN/secret sync, staged consumer rollout 뒤에만 활성화
- production primary fault injection은 수행하지 않음

## Live state at handoff

read-only preflight 결과입니다.

Primary `hololive-osaka` (`100.100.1.8`):

- Tailscale `1.102.2`
- `hololive-compose.service`: `active/exited`
- `NeedDaemonReload=no`
- `hololive-pg-fence`, `hololive-pg-route`: 아직 없음
- Tailscale Services: 없음
- `hololive-postgres.tail742dd8.ts.net`: resolve 안 됨
- current tags: 없음

Standby `vnic-kapu-iris-seoul-fk` (`100.100.1.5`):

- Tailscale `1.102.2`
- `postgres-failover.timer`: `active/running`
- controller: `--dry-run`
- `hololive-pg-fence`, `hololive-pg-route`: 아직 없음
- Tailscale Services: 없음
- `hololive-postgres.tail742dd8.ts.net`: resolve 안 됨
- tags: `tag:peer-relay`, `tag:vm`

두 PostgreSQL runtime은 18.4입니다. primary/standby replication은 이전 preflight에서 streaming,
slot active, zero lag로 확인됐습니다.

## Live rollout gates and order

현재 local environment와 stack-secrets에는 Tailscale API/OAuth credential이 없습니다. Service 생성,
approval, tag/grant 변경이 admin console action을 요구할 수 있습니다. DNS가 resolve되기 전에는 consumer
endpoint를 변경하거나 apply mode를 켜지 마십시오.

안전 순서:

1. Tailscale control plane에서 `svc:hololive-postgres`와 TCP 5433을 생성/승인합니다.
2. 기존 standby tags를 보존하면서 필요한 service host tag/grant/autoApprover를 적용합니다.
3. PostgreSQL primary/standby certificate의 기존 SAN을 보존하고 service DNS SAN만 추가합니다.
4. stack-secrets에 서로 다른 fence/route SSH key, pinned known_hosts, root-owned authorized-key files,
   route pgpass를 추가합니다. raw secret을 로그·응답·commit에 출력하지 않습니다.
5. off-host encrypted backup 후 host sync를 수행합니다.
6. dedicated sysusers, `/var/empty`, dispatcher, sudoers, root-owned AuthorizedKeysFile, sshd Match config를
   설치합니다. 실제 host에서 `sshd -t`, 계정별 `sshd -T`, 위험한 AcceptEnv 부재를 확인한 뒤 reload합니다.
7. arbitrary SSH command가 거부되고 정상 fence/route helper acknowledgement만 허용되는지 검증합니다.
8. standby route를 먼저 configure한 뒤 drain하고, primary만 advertise합니다.
9. 모든 host/container에서 service DNS와 TLS `verify-full`, exact `f|off`를 확인합니다.
10. central/AP consumer를 한 host씩 stable endpoint로 전환하고 readiness, restart count, TLS를 확인합니다.
11. direct endpoint rollback readiness를 유지한 상태에서 apply drop-in을 설치합니다.
12. healthy topology에서 controller normal tick 1회만 실행해 `primary_healthy`, failure count 0을 확인합니다.

## Hard safety rule

production primary를 끊거나 죽이는 fault injection은 금지입니다. repository runbook도 production primary
disconnect를 금지합니다. 실제 promotion fault는 고유 internal Docker network의 isolated PostgreSQL 18.4
harness에서만 수행하십시오. Production에서 apply mode를 켠 뒤에는 healthy normal tick만 검증합니다.

failover 후 자동 failback도 금지입니다. old primary는 fenced 상태로 유지하고 new primary에서 다시
basebackup해 streaming standby로 검증한 뒤, token-bound `postgres-primary-unfence.sh` 절차만 사용합니다.
marker를 수동 삭제하거나 old stack을 먼저 시작하지 마십시오.

## Stack-wide context

이 feature 이전의 Wave 3 작업은 Hololive PR #338로 main merge됐고 central/AP runtime은 healthy 상태로
배포됐습니다. ChatBotGo, TwentyQ, `iris-client-go`, `shared-go`, meta-repo의 이전 main 합류 작업도 완료된
상태였습니다. 이번 handoff의 미완료 범위는 Hololive Tailscale Service failover feature의 LOC split,
publish/merge, meta gitlink, live rollout입니다.

## Completion criteria

- 모든 touched/new file이 LOC threshold 안에 있음
- full pre-push exit 0
- branch push, PR checks green, main merge 완료
- local main과 `origin/main` 일치
- meta-repo가 Hololive merged SHA를 가리키는 별도 commit으로 main 반영
- Tailscale Service DNS/approval/tag/grant, SAN, secret metadata, SSH confinement 검증 완료
- all central/AP consumers가 stable endpoint로 healthy
- controller apply mode에서 healthy normal tick PASS
- production fault injection 0건
- raw secret 노출 0건
