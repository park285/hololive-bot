# T07. Nested payload room/user guard

## 목적

event payload가 room-agnostic이라는 불변식을 DB와 테스트로 보강합니다.

## 작업 대상

- migration SQL
- `dispatchoutbox/repository.go`
- payload tests

## 작업

1. top-level뿐 아니라 `payload.notification.room_id`, `roomId`, `room`, `users`를 금지합니다.
2. `marshalEventPayload()`가 room/users를 포함하지 않는 테스트를 추가합니다.
3. delivery-specific 데이터는 `delivery_context`에만 둡니다.

## 완료 기준

- event payload JSON에 room/users가 없습니다.
- delivery_context에는 room별 users가 들어갑니다.
- constraint 위반 테스트가 있습니다.

## LLM 프롬프트

event payload의 room-agnostic 불변식을 강화하십시오. JSON 전체 recursive scan은 hot path에 넣지 말고, 현재 payload schema의 nested notification 경로를 명시적으로 검사하십시오.
