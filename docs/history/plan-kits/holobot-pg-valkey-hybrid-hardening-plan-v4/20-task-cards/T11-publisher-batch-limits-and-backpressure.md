# T11. Batch limit과 backpressure

## 목적

한 번의 publish가 무한히 커지지 않게 합니다.

## 권장 config

```text
ALARM_DISPATCH_MAX_EVENTS_PER_BATCH=100
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
ALARM_DISPATCH_PUBLISH_MAX_CHUNKS_PER_TICK=10
```

## 작업

1. config를 추가합니다.
2. 너무 큰 batch는 chunking합니다.
3. chunk 수가 너무 많으면 다음 scheduler tick으로 넘기거나 bounded error를 반환합니다.
4. metric에 batch size histogram을 추가합니다.

## 완료 기준

- 단일 scheduler tick에서 무한 batch가 만들어지지 않습니다.
- chunk size가 테스트로 보장됩니다.
- huge fan-out 시 memory allocation이 bounded입니다.

## LLM 프롬프트

publisher batch size를 bounded config로 제한하십시오. 기본값은 보수적으로 두고, 테스트에서 chunk boundary를 검증하십시오.
