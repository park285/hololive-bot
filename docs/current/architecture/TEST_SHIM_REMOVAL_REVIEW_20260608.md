# Test shim removal review — delivery/dispatch

작성일: 2026-06-08
범위: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch`

## 목표

기존 dispatch 테스트에는 `deliveryTestDB`가 `Create`, `First`, `Find`, `Where`, `Model`, `Updates`, `Count`, `Delete` 같은 GORM식 fluent API를 흉내 내고 있었다. 운영 코드에서 GORM이 제거된 상태에서는 이 테스트 shim이 잘못된 신호를 만들기 때문에 제거한다.

## 변경 요약

1. `delivery_test_db_test.go`와 `dispatcher_integration_db_test.go`를 reflection 기반 fluent shim에서 명시 SQL helper로 교체한다.
2. 테스트 호출부를 `db.Create(...).Error` 같은 형태에서 `insertDeliveryTestRows(db, ...).Error`, `firstDeliveryTestRow(db, ...)`, `findDeliveryTestRowsWhere(...)`, `updateDeliveryTestRowsWhere(...)` 형태로 전환한다.
3. helper는 `pgxpool.Pool`을 직접 다룬다. primary 엔티(outbox/delivery/tracking/community-shorts/telemetry/alarm)는 명시적 type switch로 테이블·insert를 처리하고, test-local telemetry 모델(`deliveryTelemetryTest*Model`)은 `TableName()`/`db` 태그 기반 generic fallback(`deliveryTestTableName`, `insertDeliveryTestRowsGeneric`)으로 처리한다. read 경로는 pgxscan을 사용하므로 reflection이 완전히 제거되지는 않는다.
4. 기존 default 의미론은 유지한다.
   - `CreatedAt`, `UpdatedAt`, `NextAttemptAt` zero value는 `time.Now().UTC()`로 보정
   - `*time.Time`과 `time.Time`은 UTC 정규화
   - `CanonicalContentID`가 비어 있으면 `ContentID`로 보정
   - delivery/outbox/tracking pending 상태 기본값 보정
   - `id`가 zero인 auto-increment row는 `RETURNING id`로 채움
5. `newDeliveryExecModePool`은 `pgx.QueryExecModeExec` 검증 테스트가 사용하므로 유지한다.

## 검증 명령

```bash
go build ./hololive/hololive-shared/...
go test ./hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/... -count=1
```

shim 잔존 검사는 다음 기준으로 한다.

```bash
rg -n "newDeliveryTestDB\(|newDeliveryIntegrationTestDB\(|\.Pool\b|\.(Create|First|Find|Where|Model|Updates|Count|Delete)\(" \
  hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch -g '*_test.go'
```

단, `delivery_test_db_test.go`와 `dispatcher_integration_db_test.go`는 새 helper 구현 파일이므로 검사 대상에서 제외한다.
