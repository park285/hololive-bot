# Iris ↔ Hololive Bot 통신/순서 보장 분석 (2026-03-12)

## 요약

- 이 문서는 **실측 없이 코드 기준으로만** 정리한 Iris ↔ hololive bot 통신 레이어 분석입니다.
- 현재 구조에서 가장 먼저 확인할 점은 **Iris outbound worker 증가 가능 여부**가 아니라, **bot-side가 이미 same-room strict FIFO를 보장하는지 여부**입니다.
- 현재 bot-side webhook handler는 **ChatID 기준 striped queue(=room-keyed) + stripe worker** 구조로, **같은 채팅방 메시지는 같은 stripe에서 FIFO로 처리**됩니다.
  - 단, 조회형 명령은 async 실행 정책에 따라 reply가 out-of-order가 될 수 있습니다. (상태형 명령은 직렬 실행)
- 기능 정합성 측면에서 **`threadId`는 inbound webhook → bot 내부 컨텍스트 → Iris `/reply`까지 end-to-end로 전달**합니다. (빈 문자열은 전달하지 않음)
- Bot → Iris 클라이언트는 cleartext(`http://`)일 때 **HTTP/2(h2c prior knowledge)** 를 사용하도록 round tripper를 명시 구성해, on-wire 프로토콜이 코드상 보장됩니다.

## 관련 소스 위치

### Hololive Bot monorepo

- 저장소 루트: `/home/kapu/gemini/hololive-bot`
- bot runtime: `hololive/hololive-kakao-bot-go`
- dispatcher runtime: `hololive/hololive-dispatcher-go`
- shared Iris webhook/client 코드: `hololive/hololive-shared/pkg/iris`
- Iris 계약 상수/스키마: `hololive/hololive-shared/pkg/contracts/iris`

### Iris 소스 위치

- **현재 세션 기준 로컬 작업 경로**: `/tmp/kr-iris-src`
- **업스트림 저장소**: `git@github.com:park285/Iris.git`
- 참고: `hololive-bot` monorepo 안에는 Iris 앱 소스가 직접 포함되어 있지 않으며, cross-repo 연동 관점에서 별도 관리됩니다.

## 현재 통신 구조

### 1. Iris → Bot 인바운드

```text
Kakao message
  → Iris H2cDispatcher
  → POST /webhook/iris
  → bot-side WebhookHandler
  → dedup (X-Iris-Message-Id)
  → ChatID-hash striped bounded queues
  → N stripe workers (same-room FIFO)
  → Bot.HandleMessage
  → MessageIngress
  → CommandRouter / command execution
```

핵심 코드:

- Iris outbound webhook 전송:
  - `/tmp/kr-iris-src/app/src/main/java/party/qwer/iris/bridge/H2cDispatcher.kt`
- bot webhook 라우팅:
  - `hololive/hololive-kakao-bot-go/internal/app/api_router.go`
  - `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_webhook_youtube.go`
- bot-side webhook handler:
  - `hololive/hololive-shared/pkg/iris/webhook_handler.go`

### 2. Bot → Iris 아웃바운드

```text
Command execution
  → bot transport
  → iris.Client.SendMessage / SendImage
  → Iris /reply
  → Kakao reply enqueue
```

핵심 코드:

- bot transport:
  - `hololive/hololive-kakao-bot-go/internal/bot/bot_transport.go`
- bot-side Iris client:
  - `hololive/hololive-shared/pkg/iris/h2c_client.go`
- Iris `/reply` 처리:
  - `/tmp/kr-iris-src/app/src/main/java/party/qwer/iris/IrisServer.kt`
  - `/tmp/kr-iris-src/app/src/main/java/party/qwer/iris/Replier.kt`

## 현재 분석 결과

### 1. 같은 채팅방(room) 순서는 bot-side에서 ChatID 기준으로 보장 (striped queue)

- bot-side webhook handler는 `cfg.Webhook.WorkerCount` 기반으로 stripe를 만들고,
  **ChatID(=room key) 해시로 stripe를 선택**하여 enqueue 합니다.
- stripe별 queue는 **단일 worker가 소비**하므로, **같은 room(같은 ChatID)의 메시지는 FIFO로 처리**됩니다.

의미:

- “Iris outbound worker를 늘리면 순서가 깨질까?”를 논의할 때,
  **bot-side webhook ingest 단계에서는 same-room FIFO가 보장**된다는 점을 전제로 할 수 있습니다.
  - 다만, bot 내부에서 일부 조회형 명령을 async로 실행하면 reply 순서는 달라질 수 있습니다.

### 2. 순서 민감 명령은 room-keyed 직렬화 검토가 필요

특히 아래 성격의 명령은 순서 민감도가 높습니다.

- `alarm` 계열: add/remove/clear/list
- `member news subscription` 계열
- `major event subscription` 계열
- 앞으로 추가될 상태형/단계형 명령

반면 아래는 상대적으로 독립적입니다.

- 단순 조회형 (`help`, `live`, `schedule`, `member_info` 등)
- 순서가 바뀌어도 상태 불일치가 크지 않은 read-mostly 명령

실무적 결론:

- **same-room strict ordering이 필요하면**
  - global worker 증가보다 먼저
  - `ChatID` 기준 room-keyed executor 또는 striped worker가 필요합니다.

### 3. bot-side readiness ping은 `/config` 대신 `/ready`가 더 적절

- 현재 bot lifecycle의 Iris readiness wait는 `irisClient.Ping()`을 반복 호출합니다.
- 현재 `Ping()`은 `/ready` GET을 사용합니다.

문제:

- readiness 확인 용도로는 불필요하게 무거움
- 인증 + JSON 응답 생성 비용 사용

권장:

- readiness 확인은 `/ready` 또는 `/health`로 유지하는 것이 적절합니다.

### 4. bot-side inbound dedup cache I/O가 ACK latency에 직접 포함됨

- `/webhook/iris`는 JSON decode 후 바로 Valkey `SET NX` dedup을 수행합니다.
- 이 단계가 끝나야 enqueue/200 응답으로 넘어갑니다.

문제:

- cache가 느리면 bot-side ACK가 느려지고
- 그 지연은 Iris 재시도/백프레셔를 키울 수 있음

권장:

- dedup cache call에 짧은 timeout 부여
- 또는 in-process short-TTL dedup을 1차로 두고 Valkey를 2차 보호막으로 사용

### 5. `threadId` 지원이 필요

현재 상태:

- inbound webhook에서 `threadId`를 수신하고,
- bot 내부 `CommandContext`/context에 보존한 뒤,
- text reply 전송 시 `/reply`에 `threadId`를 함께 전달합니다. (빈 문자열은 전달하지 않음)

영향:

- 스레드/답글 문맥이 있는 대화에서 회신 정합성이 떨어질 수 있음

필요 변경:

1. inbound webhook에서 `threadId` 수신
2. bot 내부 컨텍스트에 `threadId` 보존
3. bot transport가 `iris.WithThreadID(...)`로 `/reply`에 전달

즉, **thread 지원은 반영되어 있습니다.**

### 6. bot-side `H2CClient`는 실제 h2c/http2 사용이 코드상 명시 보장되지는 않음

현재 상태:

- baseURL이 `http://`(cleartext)일 때, client는 `http2.Transport(AllowHTTP + DialTLSContext)`를 사용해 **HTTP/2(h2c prior knowledge)** 로 통신합니다.
- baseURL이 `https://`일 때는 `http.Transport`를 사용합니다.

권장:

- cleartext 환경에서 HTTP/2를 사용해야 한다면, 위처럼 round tripper를 명시 구성하고,
- 테스트/통합 테스트에서 `ProtoMajor==2`를 확인하는 것이 안전합니다.

### 7. Iris-side 추가 코드 최적화 여지

- `H2cDispatcher` timeout 조정 여지
- worker count 실험(단, bot-side ordering 설계와 같이 판단)
- `Replier` image reply 경로의 base64 double-decode 제거

## 정리된 판단

### 지금 바로 해도 되는 것

1. (완료) bot-side `Ping()`를 `/ready`로 유지
2. (완료) inbound dedup cache timeout/경량화
3. (완료) `threadId` end-to-end 전달
4. (완료) 샘플 config 문서 drift 정리 (`/webhook/iris`)

### room 순서 보장이 필요하다면 선행되어야 하는 것

1. bot-side room-keyed executor/striped worker 설계
2. 상태형 명령 범위 정의
3. 필요 시 reply 경로도 room 단위 serialize

### 순서 보장이 절대적이지 않다면 그 다음 가능한 것

1. Iris outbound worker를 2부터 실험
2. timeout 단축
3. 필요 시 worker 추가 확대

## 권장 다음 단계

1. **문서 기준 결정**
   - “같은 room strict ordering이 필요한가?”를 먼저 명시
2. **기능 정합성 우선**
   - `threadId` 지원 추가
3. **통신 최적화 1차**
   - bot-side `Ping()`를 `/ready`로 변경
   - dedup ACK 경량화
4. **그 다음 병렬화**
   - strict ordering이 불필요하면 Iris outbound worker 확대
   - strict ordering이 필요하면 room-keyed executor 설계 후 확대

## 참고

- 이 문서는 **실측/벤치마크 없이 코드 읽기 기준으로만 작성**했습니다.
- 운영값(`cfg.Webhook.WorkerCount`)과 실제 트래픽 패턴에 따라 체감 영향은 달라질 수 있습니다.
