# Transport Layer Optimization Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the missing observability and then tune the actual hot paths for internal h2c transport without regressing delivery reliability.

**Architecture:** Treat the transport path as three separate subsystems: `dispatcher-go -> Iris /reply` (Go h2c client), `Iris -> bot webhook` (OkHttp h2c client), and queue/grouping before network dispatch. Instrument each path first, then tune batching/connection reuse/timeouts using measured evidence instead of speculative changes.

**Tech Stack:** Go 1.26, Prometheus metrics, `golang.org/x/net/http2`, Kotlin/Android, OkHttp, Ktor/Netty, Valkey queueing, Docker Compose deployment.

---

## File Map

- `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
  Dispatcher group execution and the best place to add send-latency / per-room dispatch metrics.
- `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/grouping.go`
  Current room-time bucket grouping logic; candidate for coalescing policy changes.
- `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/metrics.go`
  Existing queue-only metrics; extend here before touching batching policy.
- `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
  Queue drain timing and envelope parsing path; source for enqueue-to-drain metrics.
- `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/iris/h2c_client.go`
  `dispatcher-go -> Iris` transport; connection reuse, ping, timeout, and per-request instrumentation live here.
- `/home/kapu/gemini/Iris/app/src/main/java/party/qwer/iris/IrisServer.kt`
  Iris inbound `/reply` admission server; add request latency/admission metrics and safe diagnostics here.
- `/home/kapu/gemini/Iris/app/src/main/java/party/qwer/iris/bridge/H2cDispatcher.kt`
  Iris outbound webhook transport; route-specific pools/timeouts and outbound latency metrics belong here.
- `/home/kapu/gemini/Iris/tests/`
  Shell-based operational smoke checks. Add reusable remote/local h2c verification scripts here if automation is needed.

## Chunk 1: Observability First

### Task 1: Add dispatcher send-path metrics

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- Create or modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/metrics.go`
- Test: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/*_test.go`

- [ ] **Step 1: Write a failing test for dispatch metrics registration and success/failure observation**
- [ ] **Step 2: Run only the dispatcher metric test and verify it fails for missing instrumentation**
- [ ] **Step 3: Add histograms/counters for**
  - dispatch start -> renderer done
  - dispatch start -> `/reply` response
  - per-room notification count
  - send success/failure by result
- [ ] **Step 4: Re-run the dispatcher metric test and the affected dispatcher package tests**

Run:
```bash
go test ./hololive/hololive-dispatcher-go/internal/dispatch -v
```

### Task 2: Extend queue metrics to measure backlog-visible timing

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/metrics.go`
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- Test: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/*_test.go`

- [ ] **Step 1: Add a failing test for queue timing metrics**
- [ ] **Step 2: Add metrics for**
  - drain duration
  - drain batch size
  - envelope age at dequeue if timestamp is available
  - claim release count by reason
- [ ] **Step 3: Run the queue package tests and verify green**

Run:
```bash
go test ./hololive/hololive-shared/pkg/service/alarm/queue -v
```

### Task 3: Add Iris admission latency metrics

**Files:**
- Modify: `/home/kapu/gemini/Iris/app/src/main/java/party/qwer/iris/IrisServer.kt`
- Create if needed: `/home/kapu/gemini/Iris/app/src/main/java/party/qwer/iris/IrisMetrics.kt`
- Test: `/home/kapu/gemini/Iris/app/src/test/java/party/qwer/iris/*`

- [ ] **Step 1: Write a failing test for admission timing/result accounting**
- [ ] **Step 2: Add timing and result logging/metrics around `/reply` admission**
- [ ] **Step 3: Keep the signal small: no payload logging, no room text leakage**
- [ ] **Step 4: Run targeted Iris tests**

Run:
```bash
cd /home/kapu/gemini/Iris
export ANDROID_HOME=/home/kapu/Android/Sdk ANDROID_SDK_ROOT=/home/kapu/Android/Sdk
./gradlew testDebugUnitTest --tests party.qwer.iris.IrisServerH2cTest
```

## Chunk 2: Batch and Delivery Semantics

### Task 4: Make grouping policy explicit before tuning it

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/grouping.go`
- Test: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/*_test.go`

- [ ] **Step 1: Add tests that document today’s grouping behavior**
  - same room + same scheduled minute => one group
  - same room + different minute bucket => separate groups
  - non-scheduled notifications grouped by `MinutesUntil`
- [ ] **Step 2: Only after those tests are green, add one new coalescing policy at a time**
- [ ] **Step 3: Prefer a bounded window over “aggressive” coalescing**
  Recommended first target: merge only when room, route, and scheduled minute bucket match exactly, with no semantic change yet.

Run:
```bash
go test ./hololive/hololive-dispatcher-go/internal/dispatch -run Group -v
```

### Task 5: Fix resend semantics before larger batches

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- Modify if needed: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- Test: package-local dispatcher tests

- [ ] **Step 1: Write a failing test that demonstrates current send failure behavior**
- [ ] **Step 2: Decide and document one policy**
  - either requeue failed envelopes
  - or keep current drop-on-failure semantics but surface explicit metrics/alerts
- [ ] **Step 3: Implement the minimal behavior required by the chosen policy**
- [ ] **Step 4: Verify no silent delivery loss remains unobservable**

Run:
```bash
go test ./hololive/hololive-dispatcher-go/internal/dispatch -v
```

## Chunk 3: Connection Reuse and Timeout Tuning

### Task 6: Tune Go `dispatcher -> Iris` h2c client from measurements

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/iris/h2c_client.go`
- Test: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/iris/h2c_client_test.go`

- [ ] **Step 1: Add failing tests around transport option wiring**
- [ ] **Step 2: Add configurable knobs for**
  - max idle connections / per-host if needed
  - read idle timeout
  - ping timeout
  - write byte timeout if supported safely
- [ ] **Step 3: Keep defaults conservative and internal-network specific**
- [ ] **Step 4: Verify the h2c client tests**

Run:
```bash
go test ./hololive/hololive-shared/pkg/iris -run H2C -v
```

### Task 7: Tune Iris outbound OkHttp pools per route only after metrics exist

**Files:**
- Modify: `/home/kapu/gemini/Iris/app/src/main/java/party/qwer/iris/bridge/H2cDispatcher.kt`
- Test: `/home/kapu/gemini/Iris/app/src/test/java/party/qwer/iris/bridge/*`

- [ ] **Step 1: Add route-aware latency/counter instrumentation first**
- [ ] **Step 2: Add optional per-route client config only if metrics show different behavior**
- [ ] **Step 3: Keep one shared default client until route-specific evidence exists**

Run:
```bash
cd /home/kapu/gemini/Iris
export ANDROID_HOME=/home/kapu/Android/Sdk ANDROID_SDK_ROOT=/home/kapu/Android/Sdk
./gradlew testDebugUnitTest --tests 'party.qwer.iris.bridge.*'
```

## Chunk 4: Deployment and Verification

### Task 8: Add operational smoke scripts before changing production knobs

**Files:**
- Create: `/home/kapu/gemini/Iris/tests/iris_h2c_smoke_test.sh`
- Create if needed: `/home/kapu/gemini/hololive-bot/scripts/transport/check-dispatch-metrics.sh`

- [ ] **Step 1: Script a non-destructive remote h2c smoke**
  - `GET /health` with `--http2-prior-knowledge`
  - invalid-room `/reply` expecting `400`
  - numeric fake-room `/reply` expecting `202`
- [ ] **Step 2: Script metric presence checks on dispatcher/bot endpoints if exported**
- [ ] **Step 3: Use these scripts before and after each production tuning change**

### Task 9: Roll out tuning in one narrow production batch at a time

**Files:**
- Modify only the smallest config/code subset needed for the chosen batch
- Update runbook note if operational behavior changes materially

- [ ] **Step 1: Ship observability only**
- [ ] **Step 2: Observe real traffic for at least one live alarm window**
- [ ] **Step 3: Ship one batching or timeout change**
- [ ] **Step 4: Re-run h2c smoke + real-room E2E if required**
- [ ] **Step 5: Stop if failure rate, tail latency, or claim-release anomalies rise**

## Execution Notes

- Do **not** treat `dispatcher -> Iris`, `Iris -> bot`, and `bot inbound` as one transport path.
- Do **not** tune batching before delivery failure semantics are visible.
- Do **not** add route-specific pools/timeouts until metrics show route divergence.
- Prefer one production change per rollout window.

## Fresh Verification Baseline

Use these commands before claiming a batch is complete:

```bash
go test ./hololive/hololive-dispatcher-go/internal/dispatch -v
go test ./hololive/hololive-shared/pkg/service/alarm/queue -v
go test ./hololive/hololive-shared/pkg/iris -v
cd /home/kapu/gemini/Iris && export ANDROID_HOME=/home/kapu/Android/Sdk ANDROID_SDK_ROOT=/home/kapu/Android/Sdk && ./gradlew testDebugUnitTest
cd /home/kapu/gemini/Iris && export ANDROID_HOME=/home/kapu/Android/Sdk ANDROID_SDK_ROOT=/home/kapu/Android/Sdk && ./gradlew assembleDebug assembleRelease
```

