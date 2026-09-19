# 관리자 발송 원장 운영 API

## 범위와 통합 상태

이 변경은 `hololive-api`의 관리자 API에 PostgreSQL alarm dispatch 원장 조회와
감사 가능한 재처리를 추가합니다. 기존 worker만 실제 메시지를 전송하며,
관리 API는 payload를 변경하거나 직접 외부 발송을 호출하지 않습니다.

Iris Admin은 `/dashboard/dispatch`에서 이 API를 노출합니다. `park285/iris-admin`의 OpenAPI
정본과 생성 gateway/browser 계약이 허용 경로를 소유하고, gateway가 공통 세션·CSRF·비밀번호
증명·mutation ID를 확인한 뒤 세션 audit ID를 `operatorId`로 결합합니다. 브라우저 요청 schema는
`operatorId`를 허용하지 않습니다. 조회한 묶음 리비전과 중복 위험 확인은 OperationRegistry의
단회 변경으로 전송되며 결과 불명 뒤 자동 재실행하지 않습니다. 폐기된 `admin-dashboard`나
gateway 허용 목록 우회 경로는 사용하지 않습니다.

## HTTP 계약

모든 경로는 기존 `/api/holo` API-key 인증과 rate limit 아래에 있으며,
응답에는 `Cache-Control: no-store`가 설정됩니다. 요청별 DB 작업 제한 시간은 5초입니다.

| Method | 경로 | 설명 |
| --- | --- | --- |
| GET | `/api/holo/dispatch/summary` | 보존 중인 원장 전체의 상태별 건수와 가장 오래된 생성 시각 |
| GET | `/api/holo/dispatch/deliveries` | 상태·채팅방·채널별 발송 목록 |
| GET | `/api/holo/dispatch/deliveries/{id}` | 상태, 같은 외부 발송 묶음, 재처리 가능 여부와 리비전 |
| GET | `/api/holo/dispatch/deliveries/{id}/actions` | 해당 발송의 영속 감사 이력 |
| POST | `/api/holo/dispatch/deliveries/{id}/requeue` | 조회한 묶음 전체를 원자적으로 retry에 등록 |

목록 query는 `status`, `roomId`, `channelId`, `beforeId`만 허용합니다.
상태 생략 시 `dlq`와 `quarantined`를 함께 조회합니다. 단일 상태는
`shadowed`, `pending`, `retry`, `leased`, `sending`, `sent`, `dlq`,
`quarantined`, `cancelled` 중 하나입니다. 채팅방과 채널은 정확히 일치하는 값을 사용합니다.
명시적인 빈 query, 알 수 없는 query 및 중복 query는 400입니다.

목록과 감사 이력은 최대 50건을 ID 내림차순으로 반환합니다. `nextBeforeId`가 비어 있지
않으면 다음 요청의 `beforeId`로 전달합니다. bigint ID와 집계 건수는 JSON 문자열입니다.
ID를 JavaScript Number로 변환하거나 숫자 문자열을 사전순으로 정렬해서는 안 됩니다.
페이지 사이의 전체 스냅샷은 보장하지 않으므로 새 항목을 보려면 첫 페이지를 새로 조회합니다.
감사 이력은 `beforeId` 외 query를 허용하지 않으며, 삭제된 발송 ID는 빈 목록을 반환합니다.

집계의 `oldestAt`은 상태 진입 시각이 아니라 발송 항목의 **생성 시각**입니다.
건수가 0인 상태는 `counts`에 포함되지 않습니다. 집계는 최근 24시간 같은 시간 창으로
잘라낸 값이 아닙니다. DB가 제한 시간 안에 완료하지 못하면 부분 집계를 반환하지 않습니다.

상세 응답의 `group`은 같은 send unit의 발송 항목입니다. 100건을 넘으면
`groupTruncated: true`, `replayBlocked: "group_too_large"`로 재처리를 차단합니다.
`replayBlocked`가 비어 있을 때만 `replayTargets` 전체를 요청에 사용할 수 있습니다.
메시지 본문, 오류 원문, claim key와 외부 `client_request_id`는 응답하지 않습니다.
진단용 errorCode는 제한된 코드 형태만 허용하며 나머지는 `unclassified`로 표시합니다.

## 안전한 재처리

1. 목록과 상세에서 실패 원인을 조사하고 원인을 해결합니다. 상세에서 같은 외부 발송 묶음을 확인합니다.
2. `replayBlocked`가 비어 있는지 확인하고, `replayTargets` 전체를 소수초까지 그대로 보존합니다.
3. 운영자 식별자, 사유, 명시적 중복 발송 위험 확인을 포함해 재처리를 **한 번만** 요청합니다.
4. 성공 응답 후 목록·상세·감사 이력을 새로 조회합니다. 성공은 전송 완료가 아니라 retry 등록입니다.

예시 요청 본문입니다. ID와 리비전은 임의로 만들지 말고 실제 상세 응답에서 가져와야 합니다.

```json
{
  "operatorId": "authenticated-operator-id",
  "reason": "원인 수정과 대상 확인 후 재처리",
  "duplicateRiskAck": true,
  "targets": [
    {"id": "9007199254740993", "updatedAt": "2026-09-18T01:02:03.456789Z"}
  ]
}
```

`operatorId`는 현재 backend의 API-key 신뢰 경계에서 호출자가 제공하는 감사 식별자입니다.
backend가 사용자 세션을 검증한 값이라는 의미가 아닙니다. Iris Admin 연결 시에는
브라우저가 임의 입력한 값을 전달하지 말고 gateway의 인증된 세션 사용자에 결합해야 합니다.
사유는 공백만으로 구성할 수 없고 최대 1,024자이며 제어 문자를 허용하지 않습니다.
운영자 식별자는 최대 128자, 대상은 최대 100개, JSON 본문은 최대 64 KiB입니다.
알 수 없는 JSON 필드와 중복 JSON 필드는 거부합니다.

같은 send unit의 **모든 항목**이 DLQ 또는 격리 상태여야 합니다. 이미 전송되거나 취소된
표식이 남은 항목도 차단합니다. 일부만 선택하거나 리비전이 달라진 요청은 409입니다.
독립적인 이전 형식의 발송은 단일 항목으로 처리합니다. 여러 독립 묶음을 한 요청에 섞을 수 없습니다.

직렬화 트랜잭션에서 ID 순서로 행을 잠그고 현재 리비전을 비교한 후, 상태 전환과
항목별 `manual_requeue` 감사를 같은 트랜잭션으로 커밋합니다. 감사 INSERT가 실패하면
상태 전환도 롤백됩니다. 기존 dedupe key, send unit, client request identity, 재시도 횟수와
오류 이력을 보존합니다. worker의 활성 lease를 강제로 빼앗거나 sending 상태를 재설정하지 않습니다.

HTTP 409는 새로 조회한 후 운영자가 다시 판단해야 합니다. 타임아웃, 연결 끊김 및 500은
커밋 성공 여부를 응답만으로 단정할 수 없습니다. **자동 재전송하지 말고** 상세와 감사 이력으로
결과를 확인해야 합니다. 동일 리비전으로 재전송한 요청은 성공 후에는 더 이상 적용되지 않습니다.
HTTP 400/404/413/415/503은 각각 잘못된 입력, 없는 상세 대상, 본문 크기 초과,
잘못된 Content-Type, 의존성 부재를 나타냅니다.

## 검증

저장소가 요구하는 Go toolchain과 sibling module checkout을 갖춘 검증 환경에서 실행합니다.

```bash
go test -race ./hololive/hololive-api/internal/planes/admin/internal/service/dispatchops \
  ./hololive/hololive-api/internal/planes/admin/internal/server/api \
  ./hololive/hololive-api/internal/planes/admin/app/http

# 전용 테스트 PostgreSQL만 사용합니다. 고유 schema를 만들고 종료 시 제거합니다.
TEST_DATABASE_URL='<test database DSN>' go test -race -tags=integration \
  ./hololive/hololive-api/internal/planes/admin/internal/service/dispatchops
```

통합 테스트는 기존 dispatchoutbox 테스트의 마이그레이션 정본을 재사용합니다.
묶음의 원자적 재처리, 부분 선택 차단, 감사 실패 롤백, 동시 요청의 단일 승자,
큰 bigint ID, 숫자순 cursor 페이지, 수정 시각 불일치와 이미 전송된 형제를 검사합니다.
전체 저장소의 기존 lint/build/test/release gate를 대체하지 않습니다.

이 작업 환경에서 실행한 검증은 `model.go`와 `model_test.go`를 대상으로 한
Go 1.23.2의 단위·race·coverage 검사입니다. 해당 순수 검증 로직의 statement coverage는
100%였지만, repository·HTTP·PostgreSQL 통합 검증이나 전체 저장소 coverage를 뜻하지 않습니다.
Go 1.27 기반 전체 빌드/테스트와 Iris Admin 연결은 아직 검증되지 않았습니다.
