# Integration Tests

외부 서비스와 컨테이너를 사용하는 integration 테스트의 주기 실행 경로입니다. 기본 local CI에서는 실행하지 않으며, 야간 또는 수동 주기 작업에서 다음 단독 스테이지를 실행합니다.

```bash
RUN_INTEGRATION_TESTS=true bash scripts/ci/local-ci.sh --integration-tests-only
```

`RUN_INTEGRATION_TESTS`의 기본값은 `false`입니다. `true`일 때 스테이지가 `INTEGRATION_TEST=true`를 설정하고 다음 대상을 실행합니다.

- `INTEGRATION_TEST` 게이트: major-event summarizer, member-news summarizer, YouTube delivery dispatcher
- `TEST_VALKEY_ADDR` 게이트: YouTube producer `ingestionlease`의 real Valkey 테스트
- `-tags=integration`: alarm `dispatchoutbox`, YouTube poller `batchrepo`

`TEST_DATABASE_URL`이 없으면 스테이지가 임의의 localhost 포트에 disposable PostgreSQL 컨테이너를 기동하고 모든 DB 테스트에 해당 DSN을 제공합니다. 컨테이너에는 `ci_ephemeral_sentinel`을 생성하고 일치하는 `TEST_DATABASE_OWNER_TOKEN`을 설정하므로 `github.com/kapu/hololive-dbtest`의 소유권 가드를 우회하지 않습니다. `TEST_VALKEY_ADDR`와 `TEST_VALKEY_HOST`가 모두 없을 때도 임의 포트의 disposable Valkey를 기동해 dispatcher와 ingestion lease 테스트에 같은 주소를 제공합니다. 성공과 실패 모두 shell `EXIT` trap으로 컨테이너를 정리하며, 테스트가 성공한 경우에는 스테이지 종료 전에 즉시 제거합니다. Docker가 사용 가능해야 하며 기본 이미지는 digest로 고정된 `postgres:18-alpine`과 `valkey/valkey:9.1.0-alpine3.23`입니다. 필요할 때만 `INTEGRATION_POSTGRES_IMAGE` 또는 `INTEGRATION_VALKEY_IMAGE`로 다른 disposable 이미지를 지정합니다.

기존 전용 disposable PostgreSQL을 대신 쓸 때는 `TEST_DATABASE_URL`을 설정하고, dbtest의 소유권 검증을 위해 sentinel과 일치하는 `TEST_DATABASE_OWNER_TOKEN` 또는 명시적 외부 테스트 DB 허용인 `ALLOW_EXTERNAL_TEST_DB=true`를 함께 설정합니다. 사용자가 제공한 DB에는 스테이지가 sentinel을 만들거나 소유권 변수를 덮어쓰지 않습니다. 기존 호환 경로로 `TEST_DATABASE_URL`만 설정한 일반 local CI는 `dispatchoutbox` integration 테스트를 계속 실행합니다.

기존 Valkey를 사용할 때는 `TEST_VALKEY_ADDR=host:port`를 설정합니다. YouTube delivery dispatcher는 이 주소를 우선 사용하며, 이전 호환 입력인 `TEST_VALKEY_HOST`만 설정하면 해당 host의 `6379` 포트를 사용합니다.

LLM integration 테스트는 `CLIPROXY_API_KEY`가 없으면 skip합니다. 실행할 때는 `CLIPROXY_TEST_BASE_URL` 또는 `CLIPROXY_BASE_URL`, 필요하면 `CLIPROXY_TEST_MODEL`과 `HOLOLIVE_API_ENV_FILE`을 설정합니다. 이 경로는 외부 호출 비용이 발생할 수 있으므로 예약 작업의 승인 범위와 호출량을 먼저 확인합니다.

기본 local CI는 integration 테스트를 실행하지 않지만, 태그 대상의 컴파일 퇴행은 다음 단계로 항상 검사합니다.

```bash
go vet -tags=integration \
  ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox \
  ./hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo
```
