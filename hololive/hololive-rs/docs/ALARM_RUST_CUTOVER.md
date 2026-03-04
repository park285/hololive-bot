# Alarm Rust Cutover (Broadcast Alarm) — Archived

> 발송은 dispatcher-app으로 분리됨 (M6 cutover 2026-03-02, Go alarm-dispatcher 완전 제거)

## 현재 상태 (2026-02-27)
- Alarm Phase 3(A~G) 구현 완료 + 프로덕션 cutover 완료
  - shadow_mode 코드 전체 삭제, dedup 원자화(SET NX EX), OTel 트레이싱 추가
  - Docker → Podman rootless 이관 완료 (`docker-compose.holo-rs.yml`)
  - `cargo test --workspace`: `284 passed, 2 ignored`
  - `cargo clippy --workspace -- -D warnings`: 통과

## 목표
- Go bot는 `!알람` 명령/구독 API만 유지
- 실제 방송 알림 폴링/발송은 `hololive-alarm`(Rust)로 단일화

## 배포 기본값 (docker-compose.holo-rs.yml, Podman)
- `GO_YOUTUBE_ALARM_CHECKER_ENABLED=false`
- `GO_CHZZK_ALARM_CHECKER_ENABLED=false`
- `GO_TWITCH_ALARM_CHECKER_ENABLED=false`
- `ALARM_TWITCH_ENABLED=false` (`ALARM__ALARM__TWITCH_ENABLED`로 매핑, Twitch 루프 비활성화)
- shadow_mode: **삭제됨** (2026-02-27, cutover 완료)
- 현재 운영값: **Go checker disabled + Rust 단독 발송**
  - `GO_*_ALARM_CHECKER_ENABLED=false`
  - `ALARM__ALARM__TWITCH_ENABLED=false`

## 운영 모드 매트릭스
| 모드 | Go checker | 기대 동작 |
|------|------------|----------|
| **Cutover + Twitch OFF (현재)** | `false` | Rust 단독: YouTube/Chzzk 실제 발송, Twitch 비활성 |
| Cutover + Twitch ON | `false` | Rust 단독: YouTube/Chzzk/Twitch 전체 발송 |
| 롤백 | `true` | Go 복귀 (Rust 컨테이너 중지 필요) |

> shadow_mode는 2026-02-27 cutover 완료 후 코드에서 완전 삭제됨.

## 런타임 하드닝 포인트
- Circuit breaker env (Holodex/Chzzk/Twitch/Iris)
  - `ALARM__HOLODEX__CIRCUIT_FAILURE_THRESHOLD`
  - `ALARM__HOLODEX__CIRCUIT_RESET_SECS`
  - `ALARM__CHZZK__CIRCUIT_FAILURE_THRESHOLD`
  - `ALARM__CHZZK__CIRCUIT_RESET_SECS`
  - `ALARM__TWITCH__CIRCUIT_FAILURE_THRESHOLD`
  - `ALARM__TWITCH__CIRCUIT_RESET_SECS`
  - `ALARM__IRIS__CIRCUIT_FAILURE_THRESHOLD`
  - `ALARM__IRIS__CIRCUIT_RESET_SECS`
- Provider request timeout env
  - `ALARM__HOLODEX__TIMEOUT_SECS`
  - `ALARM__CHZZK__TIMEOUT_SECS`
  - `ALARM__TWITCH__TIMEOUT_SECS`
  - `ALARM__IRIS__TIMEOUT_SECS`
- Holodex API key env (단일 경로)
  - `ALARM__HOLODEX__API_KEYS` (JSON array string, 예: `["key-a","key-b"]`)
- Scheduler loop timeout env
  - `ALARM__ALARM__YOUTUBE_CHECK_TIMEOUT_SECS`
  - `ALARM__ALARM__CHZZK_CHECK_TIMEOUT_SECS`
  - `ALARM__ALARM__TWITCH_CHECK_TIMEOUT_SECS`
- YouTube checker는 scheduler timeout보다 1초 짧은 내부 budget으로 채널/스트림 루프를 선종료한다.
- health/readiness 역할 분리
  - `/health`: liveness(프로세스 생존) 전용, 항상 `200`
  - `/ready`: readiness(의존성 + scheduler runtime heartbeat) 전용
  - `/ready`는 startup snapshot이 아니라 `scheduler_healthy`(runtime heartbeat)를 반영하며 stale 시 `503 degraded`
  - `ALARM__ALARM__TWITCH_ENABLED=false`이면 Twitch 루프 heartbeat는 readiness 집계에서 제외됨

## 데이터 경로 보강
- Go `AlarmService`가 구독 채널 레지스트리 기준으로 아래 Valkey 해시를 동기화:
  - `alarm:chzzk_channels` (`youtube_channel_id -> chzzk_channel_id`)
  - `alarm:twitch_logins` (`twitch_user_login -> youtube_channel_id`)
- 동기화 시점:
  - bot startup
  - `AddAlarm`, `RemoveAlarm`, `ClearRoomAlarms`

## 운영 확인 체크리스트
1. `hololive-alarm` liveness `200` 확인 (`/health`)
   - `status=alive` 확인 (프로세스 생존 확인)
2. readiness `200` 확인 (`/ready`)
   - `scheduler_healthy=true` 인지 확인 (런타임 heartbeat 기반)
   - 루프 heartbeat가 stale이면 `503 degraded` 로 내려가므로 startup 이후에도 지속 확인
3. `hololive-kakao-bot-go` 로그에서 Go checker disabled 메시지 확인
4. Valkey 해시 존재 확인:
   - `HGETALL alarm:chzzk_channels`
   - `HGETALL alarm:twitch_logins`
   - `ALARM__ALARM__TWITCH_ENABLED=true`이고 `alarm:twitch_logins`가 비어있지 않으면 Twitch API credential(`TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`)이 반드시 설정되어야 함
5. 테스트 채널 구독 후 Chzzk/Twitch 라이브 알림 실제 발송 확인

## 롤백
- shadow_mode 삭제로 Rust 내 dry-run 롤백은 불가. 전체 롤백 절차:
  1. `podman-compose -f docker-compose.holo-rs.yml down` (Rust 알람 중지)
  2. `docker-compose.prod.yml`에서 `GO_*_ALARM_CHECKER_ENABLED=true` 설정
  3. `docker compose -f docker-compose.prod.yml up -d hololive-kakao-bot-go` (Go 체커 복귀)
