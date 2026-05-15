# T15. Drain budget과 loop control

## 목적

wakeup 하나에 무한 drain하거나, 반대로 backlog가 있는데 불필요하게 sleep하지 않게 합니다.

## 작업 대상

- `hololive-dispatcher-go/internal/app/runtime.go`
- dispatcher loop tests

## 작업

1. `MAX_BATCHES_PER_WAKE` config를 추가합니다.
2. processed=true이면 즉시 다음 batch를 처리하되, burst limit을 둡니다.
3. burst limit 도달 후 짧게 yield합니다.
4. processed=false이면 wakeup wait or fallback timeout으로 갑니다.

## 완료 기준

- backlog가 있으면 sleep 없이 연속 처리합니다.
- 무한 tight loop는 없습니다.
- wakeup 없는 상황에서도 fallback scan이 동작합니다.

## LLM 프롬프트

dispatcher loop에 drain budget을 추가하십시오. DB를 쉬지 않고 두드리는 tight loop와 backlog 중 불필요 sleep을 모두 피하십시오.
