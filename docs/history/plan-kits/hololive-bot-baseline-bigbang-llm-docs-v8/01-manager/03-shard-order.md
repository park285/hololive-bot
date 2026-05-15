# MANAGER-03 — 권장 shard 처리 순서

## 1단계: 낮은 위험도부터 제거

```text
SH-04 template sample data
KAK-01~KAK-04 formatter/parser
LLM schema/literal shard
formatter/render helper shard
```

낮은 위험도 shard를 먼저 끝내면 전체 over-budget 수가 빠르게 줄고, Manager가 high-risk shard에 더 집중할 수 있습니다.

## 2단계: HTTP handler와 config

```text
ADM handler shards
DSP config shards
SH config/server middleware shards
LLM route registration shard
```

이 단계부터는 테스트와 response/status 불변조건 확인이 필요합니다.

## 3단계: runtime/concurrency

```text
ADM runtime
AW runtime/scheduler
DSP runtime
KAK bot runtime/lifecycle
LLM schedulerkit/bootstrap
```

R5 shard는 한 Worker에게 한 파일 또는 한 함수만 배정합니다.

## 4단계: DB/cache/queue/retry

```text
SH alarm dispatchoutbox
SH alarm queue
SH auth/cache/acl
YT outbox/poller/tracking
```

R4 shard는 transaction, retry, claim order 확인이 끝나기 전까지 done으로 표시하지 않습니다.

## 5단계: sweep

```text
SWP-01 remaining over-budget sweep
SWP-02 residual baseline string sweep
C01~C03 final validation
```
