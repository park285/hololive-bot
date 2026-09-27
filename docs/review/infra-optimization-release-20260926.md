# 인프라 최적화 migration과 X 스페이스 진단 중앙 운영 반영 기록

`DEC-20260926-hololive-x-spaces-helper-diagnostics`와 메타 `DEC-20260926-infrastructure-optimization`의 운영 근거다. 사용자가 migration 212–216 운영 적용과 X 스페이스 진단 개선을 담은 alarm worker 배포를 승인한 범위에서 수행했다. 시각은 별도 표기가 없으면 UTC다.

대상은 hololive-osaka의 중앙 API·worker와 migration 212–216이다. AP/collector, secret, runtime-config, 데이터 행·queue는 변경하지 않았다. runtime host에서는 빌드·컴파일·Git pull을 수행하지 않았다.

## 출판과 이미지

- source: main `c0ec83395cca1d6b662428ce3f324d2ce480c38f`. [PR #530](https://github.com/park285/hololive-bot/pull/530)(`c27403f14`)과 [PR #531](https://github.com/park285/hololive-bot/pull/531)(`c0ec83395`)이 포함되며, 두 PR 모두 필수 `fast-gate`와 로컬 pre-push gate를 통과했다.
- 이미지는 이 SHA의 clean detached worktree에서 kapu `kapu-multiarch` builder로 `linux/arm64`, 각 `VERSION`, 전체 SHA `REVISION`으로 빌드했다. image label과 main SHA가 같다.
- worker image의 `/app/xspaces/src/collect.mjs`는 source와 byte 단위로 같았다. API image의 `db-migrate`에는 manifest `073`–`077`(212–216)이 포함되어 있었다.
- 배포 계약 검사 `test-three-runtime-topology.sh`, `test-compose-services.sh`, `ap-deploy-version_test.sh`, `ap-host-native-deploy_test.sh`, `systemd-compose-up_test.sh`는 통과했다. `compose-version-contract_test.sh`는 API version 주입 문구를 5개로 기대하지만 Compose에는 4개가 있어 실패한다. 현재 운영 기준 `476a15009`에서도 같은 결과이고 gate에 연결되지 않은 검사라 이번 배포 판단에 쓰지 않았다.

| 대상 | 버전 | architecture | 검증한 image ID |
|---|---|---|---|
| hololive-api | 4.0.1 | arm64 | `sha256:a969cb2321f5d86d15d46a57e8c2dc50c97d2d6db9875aff45d381931039d9ea` |
| hololive-alarm-worker | 3.2.6 | arm64 | `sha256:7758464dac9e6c073429b11dc39ea4beba456125fc4065f24417b9bf88b0af0a` |

## 중앙 전환

배포 bundle은 LiveQuery 반영 때 검토한 144개 경로에 새 migration 파일 5개를 더한 149개다. SHA256은 `3c7901ad3febd137a07e6e63b35045c749cb69196593e404d7da9e90accd09bd`이다. 전환 전에 중앙 배포 트리와 비교했을 때 다른 파일은 새 migration 5개와 `manifest.txt`뿐이었다. env·secret·logs·data·runtime-config는 포함하지 않았다.

1. `change_started_at=2026-09-26T07:13:02Z`에 기존 이미지와 배포 파일을 보존했다. 실행 중 이미지·revision·health, PostgreSQL HBA·ingress 파일 해시, 퇴역 runtime 부재와 logs/data 권한을 먼저 확인했다.
2. 이미지를 전달·load한 뒤 원격에서 ID·arm64·전체 revision을 다시 확인했다. 검증한 이미지만 `prod` tag로 지정했고 wrapper의 `config --quiet`를 통과했다.
3. migration 직전 60초를 넘는 활성 transaction은 0건이었다. repository one-shot `run --rm --no-deps --pull never -T -e POSTGRES_ADMIN_PASSWORD= hololive-db-migrate`로 role/password bootstrap 없이 실행했다. 결과는 **applied=5, skipped=72, total=77**, 종료 코드 0이다. ledger 기록 시각은 07:13:30.916–07:13:30.982다.
4. API와 worker를 각각 `up -d --no-build --no-deps --pull never`로 전환했다. wrapper health gate를 통과했다. Compose가 표시한 기존 `hololive-x-space-login` orphan 경고에는 조치하지 않았다.

| 대상 | 새 StartedAt | 확인 결과 |
|---|---|---|
| API | 2026-09-26T07:13:54.455311726Z | candidate ID/revision 일치, healthy, restart 0, OOM false |
| worker | 2026-09-26T07:14:04.281324818Z | candidate ID/revision 일치, healthy, restart 0, OOM false |

## 검증

- API `/health`, bot `/internal/ready`, LLM `/health`, worker `/health`, 중앙 collector `/ready`를 기존 healthcheck 바이너리로 확인했다.
- 전환 후 로그는 API 70행, worker 19행이었다. error/panic/permission/x509/no-such-file/OOM 표식과 warning은 0건이었다. Postgres 연결 marker는 API 4개·worker 1개, API Valkey 연결 marker는 1개였다.
- collector `c`, Postgres, Valkey의 image ID·StartedAt·healthy는 전환 전과 같았다.
- 모든 DB read는 `default_transaction_read_only=on`과 `SHOW transaction_read_only = on`을 먼저 확인했다.
  - `idx_youtube_live_absence_slots_scheduled_for`는 valid·ready이고 크기는 32 MB다.
  - 제거한 네 인덱스(772·348·207·75 MB, 합계 약 1.40 GB)는 없다.
  - invalid 또는 not-ready 인덱스는 0개다.
  - 전체 크기는 `source_observations` 15 GB → 14 GB, `source_observation_queue` 300 MB → 225 MB다.
- 현재 absence slot 재조회의 원문 SQL을 최신 slot 값으로 `EXPLAIN (ANALYZE, BUFFERS)`했다. 전환 전 channel GIN 경로는 execution 50.838 ms, shared hit 14,291, 필터 제거 14,211행이었다. 전환 후 `scheduled_for` 인덱스 경로는 execution 0.162 ms, shared hit 10, 필터 제거 0행이었다. 단일 운영 관측이며 p95 근거는 아니다.
- 90초 동안 `source_observations` 135행, `source_observation_queue` 135행, `youtube_live_absence_slots` 77행이 추가되어 쓰기 경로가 계속 동작했다.
- X 스페이스 세션은 `connected`, 후보 `accepted`, 마지막 오류 없음이었다. 갱신 시각은 07:18:06으로 전환 이후다. 전환 후 worker 로그의 X 스페이스 실패는 0건이다. 따라서 새 실패 진단(`stage`·`error_name`·`helper_output`)이 운영 로그에 찍힌 사례는 아직 없다. 이 동작은 로컬 테스트와 실제 helper 출력 smoke로 확인했다.

Fallback delta: **none**. 실패를 성공으로 바꾸거나 재시도·대체 경로를 추가하지 않았다. 운영 방에 시험 메시지를 보내지 않았다.

## 복구 지점

복구 디렉터리는 `/opt/hololive-bot/compose/rollbacks/infra-opt-20260926-c0ec83395`다. `previous-deploy-files.tar` SHA256은 `0d16b450cb8369616467b083f98ec3d3eb67957c3ddd732d94bccbbd48415f76`이고, 검증 시점에 파일이 그대로였다.

- `hololive-api:rollback-infra-opt-20260926`: `sha256:7d8c76d0b3d2b508a7c4b0a1a82520c336f8e94cd8c1d87728f0b75d5e11c7a0` (revision `52e57f478`).
- `hololive-alarm-worker:rollback-infra-opt-20260926`: `sha256:7f1234bdb9cf2ced176543ee2305259cc95ee7cfd6ce9370c6f2b2cce4e8ed11` (revision `dd1009d13`).

앱을 되돌려야 하면 보존한 배포 파일과 이미지로 복구하되 212 인덱스는 유지한다. 이전 앱은 제거한 네 인덱스를 조회하지 않는다. ledger 212–216을 모르는 이전 migrator는 다시 실행하지 않는다. 제거한 인덱스가 다시 필요하면 새 migration에서 `CREATE INDEX CONCURRENTLY`로 만든다. rollback 자체는 실행·검증하지 않았다.
