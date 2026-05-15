# T05. Stable delivery dedupe key 정리

## 목적

delivery dedupe key가 raw Valkey claim key 문자열에 의존하지 않게 합니다.

## 권장 dedupe key

```text
v2:room:{room_id}:event:{event_key}
```

필요하면 hash로 줄입니다.

```text
v2:room:{room_id}:event_sha:{sha256(event_key)}
```

## 현재 위험

claim key를 category처럼 dedupe key에 섞으면 key가 길어지고, Valkey key naming 변경이 PG dedupe semantics를 흔들 수 있습니다.

## 작업 대상

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/dedupe_key.go`
- repository tests

## 작업

1. `BuildEventKey()`는 logical event만 표현합니다.
2. `BuildDedupeKey()`는 room + event key 기반으로 만듭니다.
3. `claim_keys`는 metadata로만 저장합니다.
4. 이미 production row가 있다면 compatibility path를 문서화합니다.

## 완료 기준

- dedupe key 길이가 예측 가능하고 768자를 넘지 않습니다.
- claim key 포맷 변경이 dedupe key를 바꾸지 않습니다.
- 1 event + 여러 room에서 event key는 같고 dedupe key만 room별로 다릅니다.

## LLM 프롬프트

PG delivery dedupe key를 stable domain key로 정리하십시오. Valkey claim key는 claim_keys metadata에만 저장하고 dedupe key 구성에 직접 넣지 마십시오.
