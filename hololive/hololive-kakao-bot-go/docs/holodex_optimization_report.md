# 📄 Holodex API 연동 최적화 및 안정성 개선 보고서

**작성일**: 2026-01-19
**버전**: v2.0.2
**작성자**: Sisyphus (AI Agent)

## 1. 배경 및 문제점

### 1.1 현상
- **알람 체커 과부하**: 매분 실행되는 알람 체커가 구독 중인 모든 채널(5~6개 이상)에 대해 개별적으로 `GetChannelSchedule` API를 동시 호출.
- **장애 발생**: 순간적인 요청 폭주(Burst)로 인해 Holodex API 응답이 지연되거나, 클라이언트 측 `context deadline exceeded` (Timeout 15s) 발생.
- **악순환**: 타임아웃 발생 → 재시도(Retry) → 부하 가중 → Circuit Breaker 발동 → 정상적인 서비스(명령어 등)까지 차단.

## 2. 핵심 개선 사항

### 2.1 배치 처리 (Batch Processing) 도입
기존의 `N`회 API 호출 방식을 `1`회 호출로 통합하여 네트워크 오버헤드와 API 서버 부하를 최소화했습니다.

- **변경 전**: Loop(채널 수) → `GetChannelSchedule(channel_id)` × N
- **변경 후**: `GetChannelsLiveStatus(channel_ids[])` × 1
- **API 엔드포인트**: `/users/live` 엔드포인트를 활용하여 여러 채널의 Live/Upcoming 상태를 한 번에 조회.

### 2.2 동시성 제어 (Semaphore Pattern)
Holodex API 클라이언트 레벨에서 물리적인 동시 요청 수를 제한하는 **세마포어(Semaphore)**를 구현했습니다.

- **제한 설정**: 최대 동시 요청 수 **2개** (`MaxConcurrentRequests: 2`)
- **동작 방식**: API 요청 시 세마포어 획득 시도 → 획득 시 진행, 실패/대기 시 Context 타임아웃 적용.
- **목적**: 배치 처리가 불가능한 다른 로직이나, 폴백 상황에서 요청 폭주를 원천적으로 차단.

### 2.3 단계적 폴백 및 스로틀링 (Fallback & Throttling)
배치 API 호출이 실패할 경우를 대비한 안전한 복구 전략을 수립했습니다.

- **Fallback 로직**: 배치 호출 실패 시, 자동으로 개별 채널 조회 방식(`checkChannelsSequential`)으로 전환.
- **스로틀링(Throttling)**: 폴백 실행 시, 각 요청 사이에 **500ms의 강제 지연(Delay)**을 주입하여 "천천히 안전하게" 데이터를 가져오도록 변경.

### 2.4 설정 최적화
네트워크 지연 및 처리 시간을 고려하여 타임아웃을 현실적으로 조정했습니다.

- **Timeout**: 15초 → **25초** (증가)
- **Retry Config**: 백오프 전략 유지하되, 세마포어 대기 시간 고려.

## 3. 아키텍처 흐름도

```mermaid
graph TD
    A[알람 체커 시작] --> B{구독 채널 목록 조회}
    B --> C[Batch API 호출: /users/live]
    
    C -- 성공 --> D[결과 매핑 및 알림 생성]
    C -- 실패 --> E[순차 폴백 모드 진입]
    
    E --> F[Loop: 각 채널별 조회]
    F --> G{Semaphore 획득}
    G -- 대기 --> G
    G -- 획득 --> H[개별 API 호출]
    H --> I[500ms 지연 (Throttling)]
    I --> F
    
    D --> J[종료]
    F --> J
```

## 4. 코드 변경 내역 요약

| 파일 경로 | 주요 변경 내용 |
|-----------|----------------|
| `internal/service/notification/alarm_check.go` | `pool.New()`(병렬) 제거 → `checkChannelsBatch`(배치) + `checkChannelsSequential`(순차) 구현 |
| `internal/service/holodex/api_client.go` | `semaphore` 채널 추가, `acquireSemaphore` 메서드 구현 |
| `internal/constants/constants.go` | `HolodexTimeout` (25s), `HolodexConcurrencyConfig` 추가 |
| `internal/mq/valkey_mq.go` | `funlen` 린트 에러 해결 (메서드 분리) |
| `internal/util/retry.go` | `wrapcheck` 린트 에러 해결 (에러 래핑) |

## 5. 모니터링 및 검증

### 로그 확인 방법
```bash
# 배치 처리 성공 시
grep "Batch check completed" logs/bot.log

# 배치 실패 후 폴백 작동 시
grep "Batch API failed, falling back to sequential" logs/bot.log
```

### 기대 효과
1. **API 호출 수 90% 이상 감소** (알람 체크 시)
2. **Timeout 에러 발생 빈도 0에 수렴**
3. **Circuit Breaker 발동 방지**로 서비스 가용성 99.9% 유지
