# Runbook: hololive-api

## Role

`hololive-api`는 bot/admin/llm plane과 YouTube consume plane을 한 프로세스(단일 compose service `hololive-api`)에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress, 사용자 명령 routing, reply orchestration (port `30001`).
- LLM plane: major event/member news scheduling, LLM digest 생성, internal subscription/trigger 제공자 (port `30003`).
- Admin plane: dashboard-facing admin HTTP control plane, trigger client와 worker alarm HTTP client, `members.photo` Holodex PhotoSync (port `30006`).
- YouTube plane: observation claim/finalize, canonical persist, notification intent, live-end finalizer, retention/replay. YouTube channel photos는 `channel_photo` reducer.

## Normal status

| Check | Expected |
|---|---|
| Health (bot) | `https://127.0.0.1:30001/health` returns success through container `./bin/healthcheck` |
| Health (llm) | `https://127.0.0.1:30003/health` returns success through container `./bin/healthcheck` |
| Ready (llm) | `https://127.0.0.1:30003/internal/ready` with `X-API-Key` returns success through container `./bin/healthcheck` |
| Health (admin) | `https://127.0.0.1:30006/health` returns success through container `./bin/healthcheck` |
| Logs | no repeated webhook, Iris, DB, Valkey, LLM, or trigger errors |
| Queue | webhook를 `bot_webhook_inbox`에 commit한 뒤 처리하고 reply를 `bot_reply_outbox`에서 Iris로 전달합니다. `notification_delivery_outbox` dispatch는 소유하지 않습니다. |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | commands, admin reads/writes, subscriptions, summaries, outbox fail |
| Valkey | yes | cache/config/session/PubSub behavior degrades |
| Iris | yes | Kakao ingress/reply fails |
| selected LLM provider | partial | digest/summary generation fails where enabled |
| `alarm-worker` | partial | alarm API and proactive delivery drain depend on alarm-worker |

## Compose 재생성 주의

R-12의 `log_autovacuum_min_duration=10s`는 `holo-postgres`의 compose `command` 변경입니다. 변경된 compose를 사용하는 전체 `up`이나 의존성을 시작하는 명령은 DB 컨테이너를 재생성할 수 있으며, DB 중단·재연결을 포함한 별도 운영 승인이 필요합니다.

특히 `compose-redeploy-service.sh hololive-api`도 앱의 최종 `up -d --no-deps` 전에 `run --rm hololive-db-migrate`를 실행합니다. 이 선행 명령에는 `--no-deps`가 없고 migrator는 `holo-postgres`에 의존하므로, 앱만 지정했다고 DB 재생성이 배제되는 것은 아닙니다. DB 재생성 승인이 없다면 이 compose 변경을 포함한 배포를 시작하지 않습니다. SQL 최적화 wave의 로컬 코드 승인은 R-12의 운영 활성화 승인이 아닙니다.

## Source observation replay epoch activation

Migration 191과 epoch-aware `hololive-api`/`hololive-alarm-worker` image를 epoch 부재 상태로 먼저 배포하고 normal health, source observation 처리, delivery compatibility writer를 관찰합니다. 이 단계에서는 historical coverage가 성립하지 않으며 ledger completion one-shot을 실행하지 않습니다.

Epoch activation은 irreversible production data write이고 통합 `hololive-api` 중지와 central/AP collector publish quiesce를 동반합니다. 현재 승인에 정확한 대상 host·collector fleet·중지·epoch insert·재시작이 모두 포함된 경우에만 다음 순서를 실행합니다.

1. 모든 collector publish를 quiesce하고 API가 이미 claim한 observation을 drain합니다. active claim과 current queue 상태를 read-only로 감사합니다.
2. `hololive-api`를 중지하고 epoch-aware image revision이 사전 관찰한 revision과 같은지 다시 확인합니다. `hololive-alarm-worker` compatibility writer는 계속 실행합니다.
3. 아래 one-shot을 한 번 실행합니다. `activated-by`와 `reason`에는 계정/변경 ticket처럼 비밀이 아닌 bounded audit metadata만 넣습니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  run --rm --no-deps \
  --entrypoint ./bin/source-observation-replay-epoch \
  hololive-api \
  --activated-by='<operator-handle>' \
  --reason='<approved-change-ticket>'
```

4. 출력의 `activated=true`와 non-zero `cutoff_received_at`을 확인합니다. 재실행에서 `activated=false`이면 기존 cutoff와 attribution이 그대로인지 확인하며 새 epoch로 간주하지 않습니다.
5. 같은 epoch-aware `hololive-api`를 `--no-build --no-deps`로 시작하고 health/readiness와 `replay_epoch_expired` audit를 확인한 뒤 collector를 재개합니다.
6. 이 epoch를 기준으로 하던 YouTube delivery ledger backfill은 운영에서 2026-09-01 완료됐고(T18 2026-09-26 재확인: singleton `schema_version=1`, `completed_at` 있음), backfill 명령과 alarm-worker의 완료 gate는 `DEC-20260926-hololive-retired-rollback-tooling`으로 지웠습니다. Migration 227은 적용 시점에 완료 전제를 검사합니다. 적용 전 singleton 원본·복구 SQL을 보존한 뒤 migration 229는 227 적용 기록과 현재 singleton 완료 상태를 잠금 아래 다시 검사하고, state DROP과 229 적용 기록을 한 transaction으로 커밋합니다. 229 적용 기록 없이 table이 사라진 상태는 자동 복구하지 않습니다. Runner의 별도 checksum 기록이 연결 단절로 빠지면 다음 실행은 실패하므로 적용 기록과 checksum을 조사한 뒤 복구합니다.

Activation 뒤에는 epoch row를 update/delete하거나 pre-epoch API image를 시작하지 않습니다. 기존 image rollback tag는 더 이상 안전한 rollback target이 아니며, 사전 관찰한 epoch-aware image를 유지하거나 source processing을 중지한 채 fix-forward합니다.

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | bot plane HTTP/H3 port (`30001`) | yes |
| `LLM_SCHEDULER_PORT` | llm plane HTTP port (`30003`) | yes |
| `HOLOLIVE_HTTP_TRANSPORTS` | enabled transports | yes |
| `IRIS_*` | Iris URL/certs/tokens | yes |
| `LLM_SCHEDULER_INTERNAL_URL` | internal scheduler/trigger API base | partial |
| `LLM_PROVIDER` | `cliproxy` or native `gemini` provider selection | partial |
| `CLIPROXY_*` | CLIProxy endpoint, credential, model, and reasoning settings | required when `LLM_PROVIDER=cliproxy` |
| `GEMINI_*` | native Gemini endpoint, credential, model, and thinking settings | required when `LLM_PROVIDER=gemini` |
| `MAJOREVENT_*` | major event scrape/schedule config | partial |
| `STACK_WORKER_PROFILE_FILE` | strict `hololive/api` profile for `bot_webhook_inbox`, `bot_reply_outbox`, `source_observation` | yes |
| `PHOTO_SYNC_ENABLED=true` | admin plane `members.photo` Holodex PhotoSync | yes |
| `BOT_SEE_MORE_FOLD` | bot·llm plane의 조회·보고서 2건 이상과 프로필·전체 도움말 텍스트에 전체보기 패딩 적용; 0·1건 목록과 알림은 펼침. 기본 `true`, `false`는 자동 패딩 차단 (`docs/current/architecture/MESSAGE_STYLE_GUIDE.md` §8) | no |
| `CACHE_*`, `POSTGRES_*` | state dependencies | yes |
| `YOUTUBE_PLANE_RETENTION_LIVE_ABSENCE_SLOTS_DAYS` | `youtube_live_absence_slots` 보존 기간; 소스 기본 14일, production에서는 양수 | production YouTube plane |

## YouTube 관측 보존

소스 기본값에서 `youtube_live_absence_slots`는 14일이 지난 `scheduled_for` 행을 retention tick당 최대 1000건 삭제합니다. 과거 positive 재처리는 삭제된 slot을 복원할 수 없으므로 보관 기간 밖의 absence 역재생 결과는 보장하지 않습니다. 이미 session/head에 반영된 absence clock과 `youtube_live_pending_ends`는 이 삭제에 포함되지 않습니다. 오래된 `live_snapshot`이 queue에서 대기·처리 중이거나 replay 요청이 pending이면 slot 삭제를 보류합니다. 오래된 작업이 장기간 남으면 slot 크기가 계속 증가할 수 있으므로 상태를 함께 확인합니다.

운영 보존 기간을 바꿀 때는 stack-secrets master의 `hosts/hololive-seoul/hololive-bot/compose.env`를 수정해 sync한 뒤 `hololive-api`를 `--no-build --no-deps`로 재생성합니다. `hololive_youtube_plane_retention_deleted_total{table="youtube_live_absence_slots"}`와 retention 오류·tick 시간(`hololive_youtube_plane_retention_tick_seconds{phase=source|projection}`, 슬롯 획득 뒤 실행분만 포함하며 슬롯을 얻지 못한 phase는 오류로만 셉니다), `pg_stat_user_tables`의 `n_dead_tup`·autovacuum, DB/`pg_wal`/호스트 여유를 함께 봅니다. 물리적 파일 축소는 별도 유지보수입니다.

`hololive_youtube_plane_retention_backlog_age_seconds`는 삭제 후 `source_observations`의 가장 오래된 삭제 가능 행 나이를 초로 표시합니다. 보존기간을 초과한 시간은 아닙니다. 종류별 인덱스 선두 1,000행에 큐·pending replay·종료 근거, CURRENT/STAGING generation·ACTIVE lease·활성 세션 보호를 적용합니다. application은 원본과 CASCADE 삭제되므로 별도 backlog/delete counter 대상이 아닙니다. 적격 후보가 없다고 확정하면 0으로 갱신합니다. 보호된 행이 조회 한도를 채우거나 조회가 실패하면 시계열을 생략하므로 지표 없음은 적체 없음이 아닙니다. 뒤 단계가 실패해도 앞 단계의 커밋된 삭제량은 counter에 남고 오류 counter는 실패한 테이블에만 증가합니다.

`hololive_youtube_plane_pending_queue`·`hololive_youtube_plane_processing_queue`·`hololive_youtube_plane_queue_oldest_age_seconds`는 5초마다 `source_observation_queue`를 조회해 갱신합니다. 조회가 실패하면 세 값은 마지막 성공 값을 유지하고 `hololive_youtube_plane_queue_snapshot_success`가 0이 되며, `hololive_youtube_plane_queue_snapshot_last_success_timestamp_seconds`는 마지막 성공 시각에 머뭅니다. success가 0이거나 마지막 성공 시각이 오래됐으면 세 값을 현재 적체로 해석하지 않습니다.

기본 보관 정책은 일반 원본(live/community/video/shorts) 7일, schedule 14일, profile/photo 30일, live-check 2일입니다. terminal queue는 PROCESSED 1일·DEAD_LETTER 14일, collision/replay audit 30일, 과거 checkpoint 2일, RETIRED projection 2일입니다. application은 원본 삭제와 동시에 CASCADE 삭제되며 추가 고아 유예가 없습니다. 큐·pending replay·live head/end candidate·CURRENT/STAGING generation·ACTIVE lease·LIVE/UPCOMING 세션의 application/pending-end 근거·최신 checkpoint 보호는 유지합니다. tick 120초와 배치당 1000행은 유지합니다. RETIRED projection만 한 tick에서 최대 64개 배치를 독립 commit하며 전체는 기존 DB 작업 시한(기본 10초)을 공유합니다. 배치마다 reasons+targets 합계 최대 1000행, lease 최대 1000행이며 CURRENT/STAGING과 lease 참조 세대는 보존합니다. 진척 없음·오류·취소에서 중단하고 실패한 문장을 재시도하지 않습니다. 뒤 배치 오류에도 확정된 삭제 계수와 오류를 함께 기록하며 source retention은 독립 실행합니다. 소스 기본값은 운영 master에 지정된 값을 자동으로 바꾸지 않습니다.

한 세대의 마지막 배치가 1000행 미만이어도 다른 만료 세대가 남을 수 있으므로 이를 전체 backlog 종료로 해석하지 않습니다. 64배치는 유입 증가에 대한 무제한 처리 보장이 아니며, 신규 세대·target/reason 생성량과 실제 삭제량·tick 시간·DB 크기를 함께 확인합니다. 특히 TTL 안의 이력은 삭제하지 않으므로 배포 직후 파일 크기 감소나 기존 용량 절감 목표 달성을 보장하지 않습니다.

## 동기 집계 정합 확인

`scripts/maintenance/check-runtime-projections.sh`는 `youtube_collection_runtime_bundles`와
`youtube_live_runtime_state`를 원본에서 재계산한 값과 비교합니다. dbtest와 같은 SQL 자산을 사용하며,
bundle의 observation-to-job 매핑도 운영 view와 독립적으로 비교합니다.

승인된 대상 DB의 libpq 접속 설정(`PGSERVICE` 또는 명시적 host·database·user와 기존 인증)을
준비한 환경에서 아래 명령을 실행합니다. 대상 선택·인증·원격 실행은 `stack-platform-ops`를 따르며,
스크립트는 비밀 파일을 직접 읽거나 권한을 변경하지 않습니다. 로컬 `psql`·Bash가 필요합니다.

```bash
bash scripts/maintenance/check-runtime-projections.sh
```

접속 후 `default_transaction_read_only=on`을 설정하고, `REPEATABLE READ READ ONLY` 트랜잭션에서
`SET LOCAL`로 문장당 10초·잠금 1초·idle-in-transaction 15초를 적용합니다. 읽기 전용 및 시간 제한의
유효값을 확인한 뒤 두 모델을 같은 snapshot에서 읽습니다. 연결 제한은 환경변수가 아닌 명시적
`connect_timeout=5` 인자로 전달하므로 서비스 파일의 무제한 설정이 덮어쓰지 못합니다.
[libpq 계약](https://www.postgresql.org/docs/18/libpq-connect.html#LIBPQ-CONNECT-CONNECT-TIMEOUT)에 따라
연결 5초는 각 host/IP의 시도별 제한이며, 여러 대상 전체의 합산 제한은 아닙니다.
서비스의 대상·인증과 다른 접속 옵션은 보존합니다. 원본·집계·영수증의 SELECT와 기존 영수증 조회 함수의
EXECUTE 권한이 필요하며, 권한 오류에 다른 역할로 재접속하지 않습니다.

출력은 모델별 `missing`·`extra`·`changed` 건수와 bundle의 `mapping_changed`뿐입니다.
`mapping_changed`는 값 또는 행의 중복도가 다른 observation kind의 개수입니다. 값 변경의
양방향 차이는 한 kind로 세며, 중복 행과 잘못된 NULL key도 검출합니다.
식별자·제목·payload·전체 JSON을 출력하거나 집계표를 재작성하지 않습니다.

| 종료 코드 | 의미 |
|---|---|
| 0 | 모든 비교가 완료됐고 불일치 없음 |
| 1 | 비교 완료, 하나 이상의 불일치 발견 |
| 2 | 접속·권한·timeout·guard·결과 형식 오류로 정합성을 확정할 수 없음 |

원본 재계산 비용이 있으므로 기존 5초 metrics 조회에 넣지 않습니다. 수동 점검 도구이며 자동 주기·경보는
설치하지 않습니다. 운영 데이터 규모에서 비용을 확인한 뒤 별도로 주기를 정하고, 실패를 0건으로 저장하지 않습니다.
불일치가 있으면 원본·trigger·migration 상태를 조사하며 자동 복구나 제한 시간 증액을 하지 않습니다.

## YouTube 관측 저장 구조 전환

Manifest의 **244 → 234–243** 순서와 API·collector fleet a/b/c/d·alarm-worker를 하나의 승인된 점검 창에서 전환합니다. 파일명 정렬로 적용하지 않습니다. **전체 backfill 동안 writer 정지가 필요하며**, 정지 시점과 같은 전체 복구본을 만들면 백업·복원 검증 시간도 중단 창에 포함됩니다. 온라인 backfill이나 짧은 중단을 보장하지 않습니다. 새 이미지·native artifact를 kapu에서 검증한 뒤에만 배포하며, 구현 완료와 운영 활성화를 구분합니다.

1. `stack-platform-ops`의 읽기 전용 guard로 실제 revision/ledger, 통계 source/queue/replay·MILESTONE 원장, metadata ACTIVE lease, 디스크/DB/WAL을 재확인합니다. 2026-09-29 10:19 UTC에는 MILESTONE outbox/event/collision과 처리 중 통계 queue가 모두 0건이었습니다. 이 과거 수치는 적용 승인이나 당시 재검사를 대신하지 않습니다.
2. 정확한 복구본·복원 범위를 승인받습니다. 자동 백업은 취소되어 있으며 최신 전체 DB 복구본이 있다는 전제가 없습니다. 새 일회성 복구본 생성 또는 백업 없는 복구 불가 위험의 명시적 수용 없이 234를 실행하지 않습니다. 기존 사본 제거와 이미지 정리는 별도 승인입니다.
3. `hololive-bot-ops`로 외부 ingress/admission, collector 네 대와 API/worker의 관련 작업을 quiesce하고 in-flight 작업을 정상 종료합니다. metadata ACTIVE lease, 통계 PROCESSING, MILESTONE 발송/격리를 확인합니다. 상태를 가짜 성공으로 바꾸거나 직접 재큐잉하여 드레인을 통과시키지 않습니다.
4. 유일한 적용 경로인 `db-migrate`로 manifest를 실행합니다. 244는 application의 `(observation_kind, provider)` 임시 참조 인덱스를 동시 생성합니다. 234는 통계 업무 행을 1000건씩 commit하여 제거하고 전용 객체/어휘·명령 템플릿을 삭제합니다. 다른 kind와 profile/photo는 보존합니다. 예상 밖 durable MILESTONE event/collision은 영구 closeout 원장까지 임의 삭제하지 않고 거절합니다. 235는 부분 UNIQUE를 CONCURRENTLY 생성하고, 236은 3초 lock budget 안에서 기존 전체 UNIQUE constraint를 교체합니다. 237은 bounded projection cleanup과 table-local vacuum 설정을 적용합니다.
5. 238은 JSONB(LZ4) payload 사전과 nullable 참조를 준비하고, 239–240은 참조/미처리 행 index를 각각 CONCURRENTLY 생성합니다. 큰 DB에서 241이 `backfill incomplete`로 멈추면 241 전체는 rollback되고 240까지의 적용 기록만 남습니다. 승인된 maintenance 접속에서 `SELECT public.backfill_source_observation_payloads(1000);`을 **각각 독립 commit**하며 0을 반환할 때까지 실행합니다. `payload_id IS NULL` 잔여를 재검사합니다. 동시 backfill/삭제가 있어도 누락을 숨기지 않으며 0 반환 하나만으로 완료로 간주하지 않습니다. 실패한 concurrent build의 invalid index가 있으면 자동으로 무시하지 말고 그 index만 승인된 절차로 재생성한 뒤 재개합니다.
6. `db-migrate`를 다시 실행합니다. 241은 마지막 최대 1000건과 전체 참조 검증 후 FK/NOT NULL을 적용하고 구 payload/hash 열과 backfill 함수를 제거합니다. 242는 미처리 행 전용 임시 index를, 243은 contract 퇴역 참조 인덱스를 각각 동시 제거합니다. Cutover transaction에는 전체 FK/NULL 검증이 있으므로 대형 DB에서 짧은 종료 시간을 가정하지 않습니다. JSONB 사전의 hash는 32바이트이고 외부 hex 표현은 유지합니다. 모든 새 writer/reader를 함께 배포하고 API의 새 projection이 활성화된 뒤 collector를 재개합니다.
7. 정상 metadata profile/photo와 live/schedule/content 발행, queue age, canonical/intent·중복 방지, GC·retention 오류, p95/p99를 확인합니다. 새 TTL 적용은 master 수정/sync/재생성 승인을 별도로 따릅니다. 오류·결과 불일치, 여유 20 GiB 미만, 6시간에 5 GiB 이상 감소, 반복 timeout이면 추가 backfill/정리를 중지합니다.

`db-migrate --statement-timeout=10m`은 승인된 대용량 DDL 점검 창에만 명시적으로 사용합니다. 생략하거나 `0`이면 기존 문장당 4분이며, 음수와 10분 초과는 DB 접속 전에 거절합니다. 전체 명령 15분·세션 lock 10초와 각 migration의 더 짧은 lock budget은 유지합니다. 전역 DB 설정이나 Compose 기본값에는 이 예외를 저장하지 않습니다. 늘린 한도에서도 timeout이면 자동 증액·반복 실행하지 않고 정지 상태와 복구점을 보존한 채 다음 조치를 승인받습니다.

추가 공간 상한을 사전 과소평가하지 않습니다. backfill 중 구 payload와 고유 payload·새 index가 함께 존재하며 UPDATE dead tuple/WAL이 발생합니다. 사전 표본 이득은 최대 공간 보장이 아닙니다. `DELETE`나 `DROP COLUMN` 뒤 relation 파일이 즉시 줄어들지 않을 수 있습니다. VACUUM FULL/파일 재작성은 강한 lock·임시 여유·중단 창·복구 계획을 별도로 승인받아 시행합니다. active `pg_wal`은 직접 삭제하지 않습니다.

234 이후 통계 데이터는 구 이미지만으로 복원되지 않습니다. 241 이후 구 API/collector는 존재하지 않는 열에 접근하므로 이미지 단독 rollback도 금지합니다. 오류 때 모든 writer를 정지한 채 검증한 전체 복구본과 그 시점 schema/ledger/image를 함께 복원하거나 fix-forward합니다. TTL로 만료된 원본은 설정 원복으로 돌아오지 않습니다. 새 retry/fallback/dual writer는 추가하지 않습니다.

### 감사 CASCADE와 binary hash cutover (307–308)

- API consumer/retention과 모든 publisher를 배수·정지한 같은 점검 창에서 schema와 새 binary를 함께 전환합니다. 구 writer와 새 schema의 rolling 혼용·이미지 단독 rollback은 지원하지 않습니다.
- 307은 기존 고아 감사를 PK keyset 최대 1000행씩 실제 COMMIT하는 절차로 영구 삭제합니다. 중간 실패 뒤 재실행하면 남은 고아부터 계속합니다. 이어 orphan index/function·중복 5열을 없애고 FK CASCADE와 NOT NULL을 적용합니다. 제약 검증은 metadata DDL과 별도 autocommit이며 전체 스캔을 수행합니다. DROP COLUMN은 기존 heap을 즉시 축소하지 않으므로 application의 물리 회수에는 별도 승인된 `VACUUM FULL`이 필요합니다.
- 308은 `source_observations`, `youtube_content_absence_slots`, `youtube_live_absence_slots` 각각의 두 hash 열을 한 ALTER로 text→bytea 변환합니다. **각 표의 heap·index 전체 재작성 동안 ACCESS EXCLUSIVE**를 유지합니다. 3초 `lock_timeout`은 락 획득 대기만 끊으며 재작성 시간을 제한하지 않습니다. 새 heap/index와 WAL·복제 지연을 감당할 디스크 여유와 충분한 점검 창을 확보합니다. 각 표는 독립 원자 문장이며 완료된 표는 재실행 시 타입 가드로 건너뜁니다. 부분 완료 상태에서 구 binary를 재개하지 않습니다.
- 외부 JSON/Go·identity/replay 잠금 API의 hash는 여전히 64자 소문자 hex이며 digest equality와 observation identity는 바뀌지 않습니다. hash 변환 자체가 canonical hash profile/version 변경이나 새 관측 발행 사유가 아닙니다.
- `YOUTUBE_PLANE_RETENTION_APPLICATION_AUDIT_GRACE_DAYS`는 제거합니다. 감사 지표는 부모 삭제 계수에 간접 반영되며 별도 application 삭제량으로 합산하지 않습니다. 2026-10-14 승인에서는 백업 중단 상태를 확인한 뒤 사용자가 새 백업 없이 진행을 선택했습니다. 기존 백업·이미지·배포 트리는 보존하지만 삭제 감사/퇴역 데이터 복구나 DB rollback을 보장하지 않습니다.



## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-api
```

## Metrics

통합 단일 프로세스의 자원·연결 상태를 실측하는 운영 명령입니다. 모든 명령은 호스트(central main host)에서 실행합니다.

### Container resource / health

```bash
docker stats --no-stream hololive-api hololive-alarm-worker valkey-cache holo-postgres
docker inspect hololive-api --format '{{.RestartCount}} {{json .State.Health}}'
docker logs --tail=300 hololive-api
docker logs --tail=100 deunhealth        # restart-on-unhealthy 작동 이력
```

### Go runtime metrics (Prometheus)

`/metrics`는 **bot plane만** 평문 HTTP/1.1로 `:30091`에 노출합니다(admin/llm plane은 metrics listener 없음). Go runtime collector가 프로세스 전체(3 plane 합산)를 커버합니다. prod에서는 `API_SECRET_KEY`가 설정되어 있어 `:30091`이 loopback bypass 대상이 아니므로 `X-API-Key` 헤더가 필요합니다.

```bash
# 키 출처는 실행 중 컨테이너의 실제 env(metrics 서버가 검증에 쓰는 값과 동일).
# process substitution으로 키를 호스트 argv/ps에 노출하지 않는다.
curl -s --config <(printf 'header "X-API-Key: %s"\n' "$(docker exec hololive-api printenv API_SECRET_KEY)") \
  http://127.0.0.1:30091/metrics \
  | grep -E 'process_resident_memory_bytes|go_memstats_heap_inuse_bytes|go_memstats_heap_idle_bytes|go_gc_duration_seconds|go_goroutines'
```

- GC 압력은 `go_gc_duration_seconds`(pause 합) 증가율과 `go_memstats_heap_inuse_bytes`가 `GOMEMLIMIT`(1024MiB)에 근접하는지로 본다. GC-CPU 비중 metric의 정확한 이름은 빌드된 client_golang 버전에 따라 다르므로 `curl … | grep go_` 로 실제 노출 항목을 먼저 확인한다(검증 필요).
- pprof는 `:30061`(`HOLOLIVE_API_PPROF_ADDR`)에 있고 동일하게 `X-API-Key`가 필요하다.

### DB pool·YouTube plane DB 슬롯 지표

- `hololive_db_pool_*{pool}`: pgxpool 통계를 scrape마다 읽습니다. API는 `bot`·`admin`·`llm_scheduler`·`youtube_plane`, alarm-worker는 `alarm_worker`, collector는 `collector` pool을 냅니다. 일회성 CLI(`source-observation-replay-epoch`)는 내지 않습니다. counter는 `acquires_total`, `acquire_duration_seconds_total`, `empty_acquires_total`(idle 연결이 없어 기다린 획득), `empty_acquire_wait_seconds_total`, `canceled_acquires_total`이고, gauge는 `conns{state=acquired|idle|constructing}`, `max_conns`입니다. pool 포화는 `conns{state="acquired"} / max_conns`, 빈 pool 대기는 `rate(…_empty_acquire_wait_seconds_total[5m]) / rate(…_empty_acquires_total[5m])`로 봅니다.
- `hololive_youtube_plane_db_slot_wait_seconds{operation}`, `hololive_youtube_plane_db_slot_hold_seconds{operation}`: YouTube plane 앱 DB 슬롯(`DBOperationConcurrency`)의 대기와 보유 시간입니다. operation은 `probe`, `claim`, `consume`, `retry`, `dead_letter`, `live_end`, `projection_refresh`, `replay`, `retention_source`, `retention_projection`, `queue_snapshot`, `collection_snapshot`입니다. 대기는 획득에 실패한 경우(`acquire DB slot: context deadline exceeded`)도 포함하고, 보유는 획득한 작업만 기록합니다. 전용 pool의 최대 연결 수가 슬롯 수보다 커서 슬롯 포화는 `pool="youtube_plane"` 지표에 드러나지 않으므로, 슬롯 고갈은 이 두 지표의 operation별 분포로 진단합니다.
- 슬롯을 잡는 claim·consume·retry·dead letter·live end·projection refresh·replay·retention은 슬롯 대기와 실행을 합쳐 `TransactionTimeout`(기본 10초) 안에 끝나야 합니다. 넘으면 슬롯을 돌려주고 `context deadline exceeded` 오류를 남기며, 각 loop는 기존 분류대로 오류 로그를 남기고 다음 주기를 계속합니다. 기동 중 첫 projection refresh가 이 시한을 넘으면 YouTube plane 생성이 실패합니다. startup claim probe와 queue·collection 관측 조회는 이 시한을 쓰지 않습니다.
- `hololive_youtube_plane_consume_total{result}`, `hololive_youtube_plane_retention_errors_total{table}`, `hololive_youtube_plane_retention_deleted_total{table}`는 알려진 라벨 값을 기동 때 0으로 노출하므로, 재시작 뒤 첫 retry·dead letter·retention 실패도 `increase()`에 잡힙니다.
- `youtube_live_runtime_state`는 writer가 같은 트랜잭션 끝에서 명시적으로 갱신합니다(migration 328). 정합성 점검은 `scripts/maintenance/queries/live_runtime_state_drift.sql`(누락·잉여·값 변경 행 수)이며 정상값은 모두 0입니다. 0이 아니면 해당 영상 key를 빠뜨린 writer 경로가 있다는 뜻이므로 exclusive 전역 잠금 아래 변경 key로 `refresh_youtube_live_runtime_state`를 호출해 맞추고 경로를 고칩니다.
  - **328 merge·배포 조건:** 운영에서 325의 key 비교 지표(`hololive_youtube_live_runtime_key_audit_{trigger_keys,missing}_total{path}`, 325 포함 이전 이미지)로 관찰 창 안에 `live_snapshot`·`schedule_snapshot`·`video_list`·`video_live_check`·`live_end_finalizer` 각각의 trigger_keys 증가 > 0, 전 path missing 증가 0이 확인되고, 같은 시점의 `bash scripts/maintenance/check-runtime-projections.sh` 결과가 `live_runtime_state missing=0 extra=0 changed=0`이기 전에는 328 전환을 merge·배포하지 않습니다. 328 이미지에는 이 지표가 없습니다. `video_list`는 새 예정 Premiere의 첫 분류에서만 key를 내므로 관찰 기간에 자연 입력이 없을 수 있습니다. 이 path는 격리 PostgreSQL에서 실제 publisher·consumer로 같은 경로를 트리거한 검증으로 대체합니다. 대체 검증은 325 schema의 trigger_keys 증가·missing 0, 328 schema의 drift 0, refresh 누락 음성 대조입니다.
  - 배포 순서는 API 정지 → `hololive-db-migrate`(328) → 새 API 시작 → `bash scripts/maintenance/check-runtime-projections.sh`에서 `live_runtime_state missing=0 extra=0 changed=0` 확인입니다. 328은 트리거 DROP(ACCESS EXCLUSIVE)부터 전체 재계산·COMMIT까지 `source_observations`와 라이브 원본 표 잠금을 유지하므로, migration 동안 원격 collector의 source 관측 INSERT와 worker·bot의 세션 읽기도 일시 대기할 수 있습니다. 328 뒤 이전 API 이미지만 되돌리는 롤백은 지원하지 않으며(트리거가 없어 수명 모델이 갱신되지 않음) 복구는 수정 이미지로 전진합니다.
  - 수동 검토 영수증(`record_youtube_live_review`)은 READ COMMITTED 트랜잭션에서 실행합니다. SERIALIZABLE·REPEATABLE READ에서는 `live review requires read committed transaction`으로 거절됩니다.

### 외부 HTTP 호출 지표

- `hololive_http_client_requests_total{client,code,method}`, `hololive_http_client_request_duration_seconds{client,method}`: 외부 API HTTP 요청마다 전송 계층에서 기록합니다. API와 alarm-worker의 Holodex client는 `client="holodex"`, collector는 provider 이름(`holodex`, `hololive_official`)을 씁니다. `code`는 응답 상태 코드이고, 응답을 받지 못한 요청(연결 거부·timeout·취소)은 `code="error"`입니다. 재시도한 시도도 각각 셉니다. 지연은 응답 헤더 도착이나 전송 실패까지이며 본문 읽기는 포함하지 않습니다. 오류율은 `sum by (client) (rate(hololive_http_client_requests_total{code=~"5..|error"}[5m])) / sum by (client) (rate(hololive_http_client_requests_total[5m]))`로 봅니다. 공식 일정 페이지 scraper와 collector의 youtube.js helper 호출은 이 지표에 포함하지 않습니다(helper는 `youtubejs_rpc_phase_duration_seconds`).

### PostgreSQL connections (PG18)

컨테이너 내부 socket-trust로 admin user(`POSTGRES_ADMIN_USER`, 기본 `postgres_admin`)로 접속해 조회합니다.

```bash
docker exec holo-postgres psql -U postgres_admin -d hololive -c \
  "SELECT usename, client_addr, state, count(*) \
     FROM pg_stat_activity WHERE datname='hololive' \
    GROUP BY usename, client_addr, state ORDER BY usename, client_addr, state;"
```

- **중요**: pgx DSN에 `application_name`을 설정하지 않으므로, `hololive-api`의 bot/admin/llm 3 plane은 같은 process·같은 usename(`hololive_runtime`)·같은 `client_addr`(컨테이너 IP 1개)로 보입니다 → **plane 단위 구분은 pg_stat_activity로 불가능**합니다. 구분 가능한 경계는 `client_addr`(hololive-api vs alarm-worker vs migrate) 수준입니다. plane별 budget은 정의값(bot/admin/llm 각 max 4, 합 최대 12)으로 추적합니다.
- 전체 budget은 `scripts/ci/check-postgres-capacity.sh`가 `hololive-api` bot/admin/llm 12 + YouTube plane 2 + `alarm-worker` 8 + collector 4×7=28 + migrator 1 + postgres_exporter 2 + nightly 2 = 55로 셉니다. reserve는 `max_connections=61`에서 superuser 예약 3(`superuser_reserved_connections` PostgreSQL 기본값, policy `@superuser-reserved|3`)을 뺀 비슈퍼유저 슬롯 58 대비 여유이며, 앱 역할(NOSUPERUSER)이 실제로 접속 거부당하는 조건과 같은 기준입니다. 현재 여유는 3이고 gate 하한도 3입니다. exporter 2는 postgres_exporter 0.20.x가 legacy 연결 1개를 유지한 채 scrape마다 collector 연결 1개를 더 여는 동작에 맞춘 값으로, 1이면 매 scrape가 `too many connections for role`로 끝나 pg_up만 남습니다(2026-10-19~24 실측). 하한 상향과 collector 기본 max 축소는 풀 사용 지표(acquire 대기, 최대 사용 연결)를 확인한 뒤 같은 변경에서 결정합니다. compose가 `superuser_reserved_connections`를 바꾸면 `--verify-compose`가 policy pin과 불일치로 거부합니다.

### Valkey latency / slowlog

비밀번호를 호스트 process list에 노출하지 않도록 컨테이너 내부 env(`CACHE_PASSWORD`)로 인증합니다.

```bash
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock slowlog get 25'
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock --latency'   # Ctrl-C로 종료
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock info commandstats'
```

## Common failure modes

### 1. Health check fails

Symptoms:
- Compose marks `hololive-api` unhealthy.
- Webhook replies, admin dashboard calls, or scheduler triggers stop.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
```

Mitigation:
- Check PostgreSQL, Valkey, Iris env/cert availability.
- Redeploy only after confirming config is correct.

Rollback:
- Use `docs/current/runbooks/rollback.md`. Durable runtime cutover 이후에는 이 문서의 [Rollback](#rollback) backlog preflight 없이 이전 `hololive-api` image/config를 재배포하지 않습니다.

### 2. Member news / major event command or manual trigger fails

Symptoms:
- Bot-plane command path returns scheduler/internal API errors.
- Admin manual trigger endpoint returns failure or `409 notification_in_progress`.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
```

Mitigation:
- Validate `LLM_SCHEDULER_INTERNAL_URL`, the selected LLM provider, and member news/major event source state.
- Use `LLM_PROVIDER=gemini` for Gemini native `google_search`; routing Gemini through CLIProxy does not satisfy the MajorEvent search-call contract.
- The native Gemini path uses the beta Interactions API and fails closed on non-`completed`, malformed, empty, or non-JSON output. Use `LLM_PROVIDER=cliproxy` with the retained CLIProxy settings for rollback.
- For `409`, wait for the active run to finish; investigate a stuck scheduler if the conflict persists.
- A scheduled major event digest whose summary fails (LLM error, empty result, or external-content guard failure) is not enqueued and is not retried automatically; the only trace is a `Failed to send weekly notification` / `Failed to send monthly notification` error log. The next scheduled run uses the next week or month key, so re-run `/internal/trigger/majorevent-weekly` or `/internal/trigger/majorevent-monthly` within the same KST week or month after fixing the cause (`DEC-20260926-hololive-source-fallbacks-retirement`).

Rollback:
- Roll back the plane/contract/config change that introduced failures.

### 3. Durable webhook or reply backlog grows

`bot_webhook_inbox`의 `pending`/`retry`와 `bot_reply_outbox`의 `pending`/`retryable_pre_dispatch`가 지속 증가하면 PostgreSQL, command handler, Iris 상태를 함께 확인합니다. `dead`는 poison payload 또는 bounded retry 소진, `outcome_unknown`은 부수 효과 결과를 확정할 수 없어 자동 재실행하지 않은 상태입니다. `accepted`는 Iris `202` admission과 `requestId` 저장을 마치고 lease 없이 handoff 확인을 기다리는 상태입니다. lease heartbeat가 살아 있는 command는 reclaim 대상이 아닙니다.

Reply 발송은 admission(`202`)에서 끝나고 `/reply-status`를 polling하지 않습니다. 같은 API process의 `durable-reply-handoff-reconciler`가 `accepted` 행을 `FOR UPDATE SKIP LOCKED` batch로 골라 저장된 `iris_request_id`의 `/reply-status`를 조회합니다. 확인 간격은 봇 전용 250ms→500ms→1s→2s→5s→15s→60s이며 지평은 `bot_reply_outbox` executor `attempt_timeout`입니다(새 설정 key 없음). 알림 워커의 5s→15s→60s→5min과 다른 이유는 같은 방 순서(`accepted` 선행 행이 후속 응답을 막는 권고안 A)를 유지하기 때문입니다. 5초부터 확인하면 대화형 후속 응답이 매번 최소 5초 막힙니다. 결과는 다음과 같습니다.

| Iris 관측 | 전이 | `last_error` |
|---|---|---|
| `handoff_completed` | `handoff_completed`(payload 제거) | 빈 값 |
| `failed`, attempt·144시간 예산 남음 | `retryable_pre_dispatch`. 다음 발송의 SDK ladder가 409 `CLIENT_REQUEST_ID_FAILED`를 지나 `:r1`/`:r2`로 보내며 세대 상한 2를 넘지 않음. 세 세대 모두 409이면 `permanent_conflict` | `handoff_failed` |
| `failed`, 예산 소진 | `dead`(payload 제거) | `handoff_failed` |
| `outcome_unknown`, 알 수 없는 상태, 빈 응답, 다른 `requestId` | `manual_review`(payload 보존) | `handoff_outcome_unknown` |
| `404` | `manual_review`(payload 보존) | `handoff_status_missing` |
| 진행 중 상태, 지평 전 | 전이 없이 다음 확인 예약 | 변경 없음 |
| 진행 중 상태 또는 조회 오류, 지평 도달 | `manual_review`(payload 보존) | `handoff_unconfirmed` |
| 조회 오류(timeout·5xx 등), 지평 전 | 전이 없이 다음 확인 예약 | 변경 없음 |

Reply outbox의 `outcome_unknown`은 같은 `client_request_id`로 최대 5회, 최초 dispatch인 `first_attempt_at`부터 144시간 미만인 동안에만 durable backoff 후 재확인합니다. attempt 또는 시간 경계에 도달한 행과 위 표의 확인 불가 행은 자동 발송 없이 payload를 보존한 `manual_review`로 이동합니다. 만료 lease reclaim은 `submitting`만 다루며 attempt·시간 경계에 닿은 행만 `manual_review`로 옮깁니다. Reply outbox의 attempt `success`는 Iris admission(`202`) 완료를 뜻하며 handoff 완료를 뜻하지 않습니다. handoff 완료를 세는 지표는 없습니다. reconciler 결과는 `reply handoff failed; requeued for reissue`(WARN), `reply handoff reached non-success terminal state`(ERROR, `status`·`reason` 포함), `reply handoff status query deferred`(WARN) 로그와 `bot_reply_outbox_manual_review_backlog`·`bot_reply_outbox_manual_review_oldest_age_seconds`, `status = 'accepted'` 행 수로 관측합니다. `queue="reply_handoff"` label은 빈 claim 횟수(`bot_durable_claim_idle_total`)에만 붙습니다. Manual review는 Iris 수리 여부와 같은 `client_request_id`의 처리 이력을 조사합니다. 두 manual review 값은 durable maintenance 주기마다 갱신하며, 조회가 실패하면 마지막 성공 값을 유지한 채 `bot_reply_outbox_manual_review_snapshot_success`가 0이 되고 `bot_reply_outbox_manual_review_snapshot_last_success_timestamp_seconds`는 마지막 성공 시각에 머뭅니다. success가 0이거나 마지막 성공 시각이 오래됐으면 건수·경과를 현재 상태로 해석하지 않습니다. `bot_reply_outbox_accepted_reclaimed_total`은 엄격 CHECK 릴리스에서 삭제됐으므로 이 지표를 쓰는 대시보드·alert는 지웁니다.

엄격 shape CHECK 릴리스(migration 332·334·335)는 326·333과 handoff reconciler를 포함한 직전 릴리스가 운영에서 돈 뒤에만 적용합니다. 직전 릴리스의 lease reclaim이 이전 binary의 lease 있는 `accepted`를 `manual_review`로 옮기므로, 적용 전에 아래 결과가 0인지 확인합니다. 0이 아니면 직전 릴리스를 유지한 채 lease 만료와 maintenance 주기를 기다립니다. 332는 표를 잠근 뒤 남은 행이 있으면 SQLSTATE `55000`으로 중단하고 제약을 바꾸지 않습니다. 이전 실행이 남긴 `chk_bot_reply_outbox_state_shape_next`가 있으면 재실행은 VALIDATE·교체부터 이어 갑니다. 이 제약은 사전 조회를 통과한 같은 트랜잭션에서 추가돼 새 INSERT·UPDATE에도 적용되므로, 정상 runner 순서에서는 재검증할 위반 행이 남지 않습니다. 잠금 대기(`55P03`)나 statement timeout(`57014`)으로 중단됐다면 경합·실행 시간을 확인하고 원인을 해소한 뒤 db-migrate를 다시 실행합니다.

```sql
SELECT count(*) FROM bot_reply_outbox WHERE status = 'accepted' AND claim_token IS NOT NULL;
```

순서는 manifest대로 332(엄격 CHECK, NOT VALID → VALIDATE → 교체) → 334(`idx_bot_reply_outbox_submitting_lease_expiry` CONCURRENTLY 생성) → 335(`idx_bot_reply_outbox_lease_expiry` CONCURRENTLY 삭제) → 새 API binary입니다. 334가 실패하면 runner가 그 invalid index를 지우고 멈추므로, 원인을 해소한 뒤 db-migrate를 다시 실행합니다.

Command claim이 만료되어 `outcome_unknown`으로 닫히면 `bot_durable_command_outcome_unknown_total`이 증가하고 `inspect bot_command_executions status=outcome_unknown` action log가 기록됩니다. 자동 재실행하지 말고 해당 시점의 부수 효과를 조사합니다. Inbox reclaim이 아직 살아 있는 command claim과 만난 경우에는 inbox payload를 완료·scrub하지 않고 command stale cutoff 이후로 미룹니다.

조사 결과 재발송이 필요한 한 행만 골라 아래 operator artifact를 실행합니다. 이 artifact가 replay 가능 시간과 상태 전이의 유일한 소유자이며, 기존 `attempts` 이력을 보존한 채 `operator_replay_grants`를 1 증가시켜 추가 dispatch 한 번만 허용합니다. `operator_actor`는 64자 이하의 계정/handle, `operator_reason`은 256 bytes 이하의 ticket·incident 근거만 사용하며 secret이나 사용자 원문을 넣지 않습니다. 출력은 `replayed`, `cutoff_expired`, `invalid_operator_metadata`, `not_manual_review`, `not_found` 중 하나입니다.

```bash
# 중앙 런타임 호스트. 쿼리 파일은 /migrations 밖이라 별도 마운트가 필요합니다.
sudo -n env MIGRATIONS_DIR=/opt/hololive-bot/compose/current/hololive/hololive-api/internal/planes/bot/internal/durability/queries \
  ./scripts/runtime/db-maintenance-exec.sh \
  psql -w -X -v ON_ERROR_STOP=1 \
  -v outbox_id='<bot_reply_outbox.id>' \
  -v operator_actor='<operator-handle>' \
  -v operator_reason='<ticket-or-incident-reason>' \
  -f /migrations/reply_outbox_replay_manual_review.sql
```

Replay cutoff는 row의 `created_at`부터 144시간입니다. Iris admission retention 168시간보다 24시간 짧게 닫아 operator 판단 뒤 실제 dispatch와 sweep이 지연되더라도 dedup retention 경계를 넘지 않게 합니다. 144시간 경계와 그 이후에는 fail-closed합니다. `cutoff_expired`이면 재발송하지 않습니다. `bot_reply_outbox_replay_audit`에는 grant 시점의 `granted`, 실제 claim 시점의 `replayed` event가 같은 actor/reason과 별도 `recorded_at`으로 append되므로 이후 outbox `updated_at` 변경과 분리해 조사합니다. `invalid_operator_metadata`, `not_manual_review`, `not_found`도 상태를 임의로 고치지 말고 현재 행과 운영 이력을 다시 확인합니다.

Iris `/reply-status/{requestId}`가 `outcome_unknown`처럼 handoff 결과를 증명하지 못하고 operator가 중복 방지를 위해 재발송하지 않기로 결정한 경우에는 직접 `UPDATE`하지 않습니다. 아래 artifact는 대상이 아직 `manual_review`인지 row lock으로 재검증하고, 관측한 Iris 상태와 actor/reason을 immutable audit에 기록한 뒤 `discarded` terminal 상태로 전이하면서 payload를 제거합니다. 출력은 `discarded`, `invalid_operator_metadata`, `invalid_iris_state`, `not_manual_review`, `not_found` 중 하나입니다.

```bash
sudo -n env MIGRATIONS_DIR=/opt/hololive-bot/compose/current/hololive/hololive-api/internal/planes/bot/internal/durability/queries \
  ./scripts/runtime/db-maintenance-exec.sh \
  psql -w -X -v ON_ERROR_STOP=1 \
  -v outbox_id='<bot_reply_outbox.id>' \
  -v operator_actor='<operator-handle>' \
  -v operator_reason='<ticket-or-incident-reason>' \
  -v observed_iris_state='<reply-status-state-or-not_found>' \
  -f /migrations/reply_outbox_discard_manual_review.sql
```

`discarded`는 재발송 권한을 만들지 않고 terminal retention을 따릅니다. 실행 전에는 반드시 같은 `iris_request_id`의 최신 `/reply-status`를 조회하고, `queued`/`preparing`/`prepared`/`sending`이면 진행 중인 handoff가 끝날 때까지 보류합니다. `failed`에서 재발송이 필요하다고 판단한 경우에는 discard가 아니라 위 replay artifact와 144시간 cutoff를 사용합니다.

Durable runtime binary보다 migration 123~136을 먼저 적용해야 합니다. 실행 순서의 SSOT는 filename 정렬이 아니라 `hololive/hololive-api/scripts/migrations/manifest.txt`이며, replacement due index를 먼저 만드는 127이 기존 index를 제거하는 126보다 앞섭니다. Outbox는 같은 room의 active 선행 행을 직렬화하지만 `manual_review`는 operator 보류 상태이므로 후속 room reply를 막지 않습니다. Migration 133은 inbox terminal payload scrub과 CHECK를 도입했고, 현재 inbox writer는 terminal 전이에서 payload를 직접 비웁니다(호환 trigger는 230에서 폐기). Migration 134의 command terminal summary trigger는 그대로 유지합니다. 주기 maintenance는 scrub scan을 반복하지 않고 retention 대상만 찾으며, terminal ledger는 Iris admission retention(7일)보다 긴 8일 뒤 batch 삭제합니다. `manual_review`와 그 replay audit은 판단·처리 이력을 위해 해당 outbox row의 retention 동안 함께 보존합니다.

Migration 133은 과거 runtime cutover 전에 terminal payload scrub trigger를 설치하고 기존 `dead`/`succeeded` row를 backfill한 뒤 CHECK를 validate했습니다. Migration 223은 trigger가 구 status-only writer를 만날 때 WARNING을 남기도록 했습니다. 2026-09-28 읽기 전용 검증에서는 중앙 API 이미지 21개(서로 다른 revision 20개)와 문서화된 rollback 이미지 2개가 모두 terminal payload를 직접 비우는 네 writer(`inbox_complete.sql`, `inbox_abandon.sql`, `inbox_release.sql`, `inbox_reclaim_expired.sql`)를 포함했습니다. 이 증거는 이미지 label을 source revision에 대응시킨 것이며 바이너리 역공학이나 장기 로그 관측은 아닙니다. Migration 230은 이 지원 집합을 전제로 호환 `bot_webhook_inbox_terminal_payload_scrub` trigger와 `scrub_bot_webhook_inbox_terminal_payload()` 함수만 원자적으로 지웁니다. 검증된 `chk_bot_webhook_inbox_terminal_payload_scrubbed` CHECK는 유지합니다. 구 status-only writer는 이제 CHECK에 거절됩니다. 네 현행 writer는 terminal 전이에서 `{}`를 직접 쓰고 retry에서는 payload를 보존해야 합니다.

Migration 230은 적용 전 catalog에서 정확한 trigger/function 관계와 CHECK 정의·검증 상태를 확인합니다. 누락·변형·추가 의존성·락 실패는 적용을 중단하고 DROP을 롤백합니다. 지원 중인 두 rollback 이미지는 같은 CHECK를 직접 만족하므로 바이너리 rollback에 trigger 재설치는 필요하지 않습니다. Schema를 되돌려야 하면 다음 번호의 forward migration으로 처리하거나 ledger와 schema가 함께 일치하는 전체 복원만 별도로 검토합니다. 230 적용 기록을 둔 채 수동으로 백업 DDL만 재설치하면 runner가 230을 skip하는 schema drift가 되므로 정상 rollback 절차가 아닙니다.

Migration 125 이후 runtime은 `bot_webhook_heads`와 `ordering_key` advisory lock을 함께 사용합니다. Schema rollback은 이전 runtime으로 먼저 전환해 writer를 quiesce한 뒤에만 `bot_webhook_heads`/`available_at`을 제거해야 하며, 현재 runtime이 쓰는 동안 migration 125~136을 되돌리면 안 됩니다.

Epoch-1/R1 artifact로 되돌린 뒤 migration 114를 다시 적용하던 preflight(`preflight-114-restore.sh`)와 074~082 message contract repair 도구는 `DEC-20260926-hololive-retired-rollback-tooling`으로 epoch-1 rollback 창을 닫으며 지웠습니다. T18(2026-09-26)에서 운영 `schema_migrations`에 `001_schema_epoch2_baseline`·`182_epoch2_legacy_ledger_cleanup`이 기록되고 epoch-1 파일명 행이 0건이며 epoch-1 이미지가 보존되지 않음을 확인했습니다.

DB maintenance 명령(아래 `db-maintenance-exec.sh`로 실행하는 migration·preflight SQL, 예: [Rollback](#rollback)의 durable preflight)은 libpq service와 password file을 사용합니다. `PGPASSFILE`은 readable regular file이어야 하고 symlink는 금지합니다. `PGPASSWORD`와 connection URI command argument는 허용하지 않으며 `psql -w`로 interactive password fallback도 차단합니다.

> 중앙 런타임 호스트에는 `psql`이 없습니다. `scripts/runtime/db-maintenance-exec.sh`가
> PostgreSQL 이미지를 일회성으로 띄워 `/migrations`와 `stack-secrets`의 service/pgpass·CA를
> read-only로 마운트하고 그 안에서 명령을 실행합니다. libpq service 정본은
> `/etc/stack-secrets/hololive-bot/postgres/{pg_service.conf,pgpass}`(둘 다 `0600 root:root`)이며
> `hololive_migrator`로 `verify-full` 접속합니다. 이 스크립트를 중앙 런타임 호스트에서 실행하십시오.

### 4. Fx lifecycle startup or shutdown fails

Fx v1.24.0은 `hololive-api` 한 바이너리의 process signal과 `Start`/`Wait`/`Stop`만 소유합니다. 별도 Fx mode나 legacy lifecycle fallback은 없으며, plane 내부 계약과 운영 endpoint는 바뀌지 않습니다.

| Log diagnostic | Meaning | Required response |
|---|---|---|
| `Failed to assemble hololive-api runtime` or Fx graph/invoke error | telemetry, aggregate runtime, or Fx graph initialization failed before service start | Redacted error metadata로 실패한 constructor를 확인하고 config/dependency 원인을 수정합니다. 이미 생성된 resource는 reverse order로 once-only cleanup됩니다. |
| `OnStart hook failed` / `hololive-api Fx start failed` | process lifecycle hook did not complete within the 30-second start budget or Fx start rollback failed | listener bind와 dependency initialization 오류를 확인합니다. 일부 plane만 살아 있는 상태를 정상으로 간주하지 않습니다. |
| `hololive-api runtime error` | listener/runtime component가 fatal error를 보고하여 Fx shutdown을 요청했습니다 | 최초 runtime error를 원인으로 조사합니다. scheduled job의 자체 처리 오류는 이 경로의 process-fatal이 아닙니다. YouTube 관측 Retry·DeadLetter 기록이 일시 오류(DB slot timeout, 연결·잠금 경합)로 실패하면 종료하지 않고 `awaiting lease recovery` ERROR 로그와 `retry_error`·`dead_letter_error` 지표만 남기며, `PROCESSING` 행은 lease 만료 뒤 claim이 회수합니다. 그 밖의 기록 실패는 계속 이 경로로 종료합니다. |
| `drain runtime planes` with deadline/shutdown errors / `hololive-api Fx stop failed` | 10-second plane drain에서 하나 이상 실패했습니다. 남은 plane과 tail cleanup은 계속 시도됩니다. | bot → admin → llm → YouTube 순서의 shutdown 진단을 확인하고 실패한 plane의 listener 또는 background loop를 조사합니다. |
| `hololive-api Fx stop timed out` | 전체 30-second process stop cap이 만료되었습니다. | 같은 resource에 concurrent forced cleanup을 실행하지 않습니다. Compose의 45-second grace 이후 종료 결과와 다음 기동 상태를 확인합니다. |

진단 문자열은 redacted `error` field로 기록됩니다. raw config, token, DSN, certificate, secret을 로그에 복사하지 않습니다. 아래 기존 log 명령과 health smoke를 사용하며, restart/redeploy/rollback은 별도 승인 후 이 runbook의 기존 절차를 따릅니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30003/internal/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- migration 231~233 적용 뒤 v6.0.x image로 되돌릴 때는 교체 직전에 `auth:sess:*`를 모두 `UNLINK`해 관리자
  재로그인을 강제하고, `auth:user_sessions:*`는 rollback 창이 닫힐 때까지 TTL로 둡니다. image만 `--no-deps`로
  교체하고 구 `hololive-db-migrate`는 실행하지 않습니다(`rollback.md`의 Valkey 2차 축소 절). retag하면 migrate도 같은
  `hololive-api:prod` image의 구 runner가 되므로, rollback 창에는 중앙 host를 재부팅하지 않고 `hololive-compose.service`를
  disable합니다. 재부팅의 `systemd-compose-up.sh`는 `--no-deps` 없는 `up`이라 구 runner가 거절되고 API·worker·중앙
  collector가 기동하지 못하며, 복구는 7.0.0 image로 재전진입니다. 이 동안 `APP_VERSION`은 트리 값 7.0.0을 보고하므로
  판정은 image label로 합니다.
- Durable runtime cutover 이후 기본 복구 전략은 현재 image의 fix-forward입니다. 이전 image는 durable queue를 소비하지 않으므로 backlog가 남아 있으면 재배포하지 않습니다.
- 이전 `hololive-api` image/config가 반드시 필요하면 Iris webhook ingress를 먼저 quiesce하고 현재 durable runtime으로 queue를 drain한 뒤 아래 preflight가 성공해야 합니다. 하나라도 0이 아니면 현재 image를 유지하고 roll forward합니다.

  ```bash
  sudo -n ./scripts/runtime/db-maintenance-exec.sh \
    bash /migrations/preflight-durable-runtime-rollback.sh --ingress-quiesced
  ```

- Migration 326 이후 binary를 그 이전 binary로 되돌리면 lease 없는 `accepted` 행은 이전 reclaim(`lease_until <= now`)에 걸리지 않아 같은 방의 후속 응답을 영구히 막습니다. Ingress를 quiesce한 뒤 `bot_reply_outbox` executor `attempt_timeout` 지평 동안 reconciler가 `accepted`를 0으로 배수하면 위 preflight가 통과합니다. Preflight는 `accepted`가 남은 rollback을 막습니다. Migration 332 이후에는 326 이전 binary가 쓰는 lease 있는 `accepted`를 엄격 CHECK가 거부하므로 그 binary로는 되돌리지 않고, 326·333을 포함한 직전 릴리스까지만 되돌립니다.

- Preflight 성공과 ingress quiescence를 같은 maintenance window에서 유지한 경우에만 이전 image를 재배포합니다. Schema rollback은 계속 [`Migration ordering`](#3-durable-webhook-or-reply-backlog-grows)의 writer quiescence 규칙을 따릅니다.
- Recheck Iris webhook/reply, scheduler-dependent commands, manual triggers, and dashboard health after rollback.

## Post-deploy monitoring (unified runtime)

bot/admin/llm을 한 프로세스에 묶었으므로 평균값보다 동시 spike(예: LLM weekly digest + admin stats 조회 + bot 이미지 렌더링 동시 발생)가 중요하다. 컷오버 후 최소 24시간 관찰:

### 단일 프로세스 blast-radius (먼저 인지할 것)

- 3 plane이 한 프로세스이므로 **한 plane의 자원 폭주(OOM/goroutine leak/GC thrash)가 전체 컨테이너를 끌어내립니다.** healthcheck(`30001/health`·`30003/internal/ready`·`30006/health`)는 각 URL을 순차 검사해 하나라도 실패하면 exit 1 → unhealthy → deunhealth가 컨테이너 전체를 재시작합니다.
- `30003/internal/ready`는 인증된 dependency readiness(PostgreSQL/Valkey)를 포함합니다. 외부에서 접근 가능한 `/ready`는 dependency ping 없이 process health만 반환합니다.

### 경계값 (initial threshold — 실측으로 보정)

아래 수치는 운영 시작 기준선입니다. 실측 baseline 확보 후 조정합니다.

- **RSS**: `docker stats`의 MEM USAGE가 **1.1GiB(limit 1280m 대비)를 5분 이상 초과** 시 경고. limit(1280m)·`pids: 512` 근접 시 OOM/pid-kill 위험 → incident.
- **Heap vs GOMEMLIMIT**: `go_memstats_heap_inuse_bytes`가 `GOMEMLIMIT`(1024MiB)에 지속 근접하면 GC thrash 구간 → `go_gc_duration_seconds` 증가율 동반 확인. GC가 CPU의 ~10%를 지속 점유하면 조사(정확한 GC-CPU metric 이름은 `grep go_`로 확인).
- **bot webhook p99**: Iris webhook 타임아웃(5s) 기준. **p99가 2s를 지속 초과하면 경고, 5s에 근접하면 reply drop → incident.**
- **pgx acquire latency**: plane별 pool max 4라 contention 민감. **acquire p99 > 50ms 지속 → 경고, > 500ms → pool 고갈 임박(incident).**
- **deunhealth restart**: `docker inspect hololive-api`의 `RestartCount ≥ 1` 또는 deunhealth restart 로그 발생 시 **즉시 incident triage**(정상 운영 중에는 0이어야 함).

### 관찰 항목

- Go RSS / heap inuse·idle, GC pause·GC CPU 비중 (GOMEMLIMIT 1024MiB, 컨테이너 limit 1280m·pids 512 대비 여유) — `Metrics` 절 명령 사용
- PostgreSQL connection 수 — plane별 pool 합산(bot/admin/llm 각 max 4 + YouTube plane max 2 = 최대 14) + alarm-worker(max 8)/collector AP 4×8=32/migration 포함 전체 budget. plane 단위 구분은 불가(같은 client_addr/usename — `Metrics` 절 참조)
- pgx acquire latency, Valkey command latency(slowlog/--latency), H3 handshake error rate
- bot webhook p95/p99, admin API p95/p99, LLM scheduler job lag
- deunhealth 재시작 빈도 — 잦은 재시작은 H3 listener hang/5s 타임아웃/GC pause를 의심

## Related contracts

- `member-cache-v2-rollout.md`
- `../contracts/iris-boundary.md`
- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/trigger.md`
- `../contracts/settings.md`
- `../contracts/alarm.md`
