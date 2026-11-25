# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

## r1.2.6 - 2026-11-25

- **PostgreSQL 코드 규칙:** 다섯 모듈의 운영 SQL을 모두 `shared-go/postgres` v1.0.4의 `pgxdb.Statement`로 읽고 `pgxdb.Exec`·`Query`·`QueryRow`·`Queue`로 실행합니다. `sqlassets.MustStatementReader`가 `pgxdb.Statement`를 돌려주고 `sqlassets.WithFragment` 대신 `sqlassets.Fragment` 자리표시자와 `With`로 조각을 끼우며, `dbx.Tx`는 `pgxdb.Querier`를 품으며 `dbx.Statement.SQL`은 `pgxdb.Statement`입니다. Go 코드에서 조립하던 SQL은 `queries/*.sql`의 자리표시자와 `With`·`WithToken`으로 옮겼습니다. 433개 문장을 변경 전후로 비교해 양 끝 공백 말고는 같음을 확인했습니다. 스택 ast-grep 규칙 `go-postgres-conventions.yml` 대신 golangci-lint forbidigo가 pgx·pgxpool의 SQL 직접 실행, `BeginFunc`, 트랜잭션 직접 `Rollback`, `pgxpool.New`를 거부합니다. migration 실행·역할 bootstrap DDL, 행 수만큼 `VALUES`를 만드는 일괄 쓰기, 관리자 목록 필터만 `nolint`로 둡니다.
- **형식 검증 동기화:** `AlarmDispatchClaim` 모델에 `claim_due.sql`의 행 버전·due 재검사를 넣었습니다. 오래된 스냅샷으로 선점하는 작업자가 그 사이 회수되어 미래로 다시 예약된 retry 행을 가져가지 않는다는 `StaleClaimLeavesFutureRetry`를 검사하며, 재검사를 지운 복사본은 이 속성을 위반합니다. `CollectionJobLease`와 함께 동기화 커밋을 `5a3bb90c9`로 옮겼습니다.

## r1.2.5 - 2026-11-25

- **AP a native 창 정리:** PgBouncer 퇴역 전환이 끝나 W1·W2 창을 지웠습니다. native 도구는 W3에서 수용된 원점만 받습니다. 퇴역 전환 절차 문서와 그 링크도 지웠습니다.
- **인프라 image를 공식 image로 전환:** PostgreSQL·Valkey·nginx 자체 image와 빌드 recipe를 지우고 고정 digest의 공식 image(`postgres:19.0-alpine`, `valkey:9.1.3-alpine3.24`, `nginx:1.31.9-alpine-slim`)를 그대로 씁니다. 공식 image에 없는 `pg_stat_kcache`·`pg_wait_sampling` 설정을 unit에서 뺐고, Valkey health check는 `valkey-runtime` 대신 `valkey-cli`를 직접 실행합니다. 세 image는 주간 최종 image 스캔에서 빠집니다. 공식 image에는 아직 수정 전 Alpine 패키지(PostgreSQL의 libxml2 CVE-2026-86144 등)와 gosu의 Go 1.24.6 표준 라이브러리 보고가 남아 있습니다. 운영 전환은 [확장 제거 절차](docs/current/runbooks/postgres-observability.md#공식-image-전환-전-확장-제거)를 먼저 따릅니다.

## r1.2.4 - 2026-11-25

- **AP a 수용 readiness:** W3 수용 단계는 issuer가 준비된 뒤 collector `/ready`가 한 번 503을 돌려주면 바로 결과 불명으로 끝났습니다(2026-11-24 osaka1 정산 재개, youtube.js helper 응답 지연). 이제 기존 90초 한도 안에서 다시 확인하고, 한도가 지나면 지금처럼 결과 불명으로 끝납니다.
- **AP a 수용 단계 정산:** `--settle`은 수용이 차단 정책을 확정하기 전(정책 `observing`, `accepted-window` 없음)에 결과 불명으로 끝난 창도 받아들이고, 관찰 기록·인계·`accepting`을 함께 보관한 뒤 다시 재개합니다. issuer가 다시 기동하지 못해 socket이 `trigger-limit-hit`으로 실패했으면 그 실패 기록을 확인하고 socket을 비활성으로 되돌립니다.

