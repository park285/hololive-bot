# Hololive Alarm 고위험 변경/운영 절차 Runbook

- 작성일: 2026-02-26
- 대상 서비스:
  - `hololive-bot` (`hololive-kakao-bot-go`)
  - `hololive-alarm` (`hololive-scraper-rs` alarm app)
- 관련 컴포즈: `docker-compose.prod.yml`

---

## 1) 목적

이번 리팩토링에서 발생 가능한 고위험 구간을 명시하고, 배포/검증/롤백 절차를 표준화합니다.

---

## 2) 고위험 변경 항목 (요약)

| 항목 | 위험도 | 영향 | 즉시 확인 포인트 |
|------|--------|------|------------------|
| Holodex API key 경로 단일화 (`ALARM__HOLODEX__API_KEYS` only) | 높음 | `hololive-alarm` 기동 실패/`/ready` 503 | 환경변수 형식(JSON array string), 기동 로그 |
| AlarmService fail-fast (persist pool 생성 실패 시 에러 반환) | 중간~높음 | `hololive-bot` 기동 실패 | bot startup 로그, 컨테이너 restart loop |
| `alarmAdvanceMinutes` 런타임 반영 (`n -> [n,3,1]`) | 중간 | 알림 시점 변경/중복·누락 리스크 | settings API 반영 결과, target minutes |
| Twitch disabled health 집계 변경 | 중간 | `/ready` semantics 변경 | `ALARM__ALARM__TWITCH_ENABLED=false`에서 readiness |
| (후속 예정) dedup legacy JSON fallback 제거 | 높음 | 중복 알림/누락 알림 | writer/read 경로 일치 여부, 마이그레이션 상태 |

---

## 3) 배포 전 체크리스트 (필수)

### 3.1 환경변수/설정

1. Holodex 키
   - **필수**: `ALARM_HOLODEX_API_KEYS` (호스트 env)
   - 형식 예시:
     - `ALARM_HOLODEX_API_KEYS='["key-a","key-b"]'`
2. Twitch 비활성 운영 시:
   - `ALARM_TWITCH_ENABLED=false`
3. bot settings 파일 경로:
   - 컨테이너 내부 `/app/logs/settings.json` 사용
   - 볼륨 `./logs:/app/logs` 쓰기 가능 상태 확인

### 3.2 컴포즈 정합성

```bash
docker compose -f docker-compose.prod.yml config -q
```

### 3.3 백업

1. PostgreSQL 덤프 (hololive DB)
2. Valkey snapshot/RDB 보관
3. 현재 compose/env 파일 백업 (`docker-compose.prod.yml`, `.env`)

---

## 4) 배포 절차 (권장 순서)

### 단계 A: 이미지 빌드/배포

```bash
docker compose -f docker-compose.prod.yml up -d --build hololive-bot hololive-alarm
```

### 단계 B: 컨테이너 상태 확인

```bash
docker compose -f docker-compose.prod.yml ps hololive-bot hololive-alarm
```

### 단계 C: 로그 확인 (초기 5~10분)

```bash
docker logs --tail=200 hololive-kakao-bot-go
docker logs --tail=200 hololive-alarm
```

확인 포인트:
- bot: `failed to create alarm service`, `persist pool` 오류 없음
- alarm: Holodex key 파싱/설정 오류 없음

---

## 5) 배포 후 검증 절차

### 5.1 헬스체크

```bash
curl -fsS http://127.0.0.1:30001/health
curl -fsS http://127.0.0.1:30011/health
curl -fsS http://127.0.0.1:30011/ready
```

기대:
- `/health`: 200
- `/ready`: 200 (degraded 아님)

### 5.2 settings 동작 검증 (`alarmAdvanceMinutes`)

```bash
curl -sS -X POST http://127.0.0.1:30001/api/holo/settings \
  -H 'Content-Type: application/json' \
  -H 'X-API-Key: <API_SECRET_KEY>' \
  -d '{"alarmAdvanceMinutes":7}'
```

기대:
- 응답 `runtime.alarm_applied == true`
- 응답 `runtime.alarm_target_minutes == [7,3,1]`

### 5.3 Twitch disabled readiness 확인

운영값이 `ALARM_TWITCH_ENABLED=false`일 때:
- `hololive-alarm` 로그에서 Twitch disabled 상태 확인
- `/ready`가 `scheduler_healthy=true`로 유지되는지 확인

---

## 6) 장애 대응 / 롤백 절차

## 시나리오 A: `hololive-alarm` 기동 실패 (Holodex key)

1. 즉시 env 점검:
   - `ALARM_HOLODEX_API_KEYS` JSON string 유효성 확인
2. 원복:
   - 직전 정상 `.env`/compose 복원
   - 이전 이미지 재기동

```bash
docker compose -f docker-compose.prod.yml up -d hololive-alarm
```

3. `/ready` 정상 복귀 확인

## 시나리오 B: `hololive-bot` 기동 실패 (fail-fast)

1. bot 로그에서 `create alarm persist pool` 계열 오류 확인
2. 자원 상태 확인(메모리/파일 핸들/workerpool init 환경)
3. 즉시 원복 배포(직전 정상 이미지/설정)

## 시나리오 C: 알림 시점 이상 (`alarmAdvanceMinutes`)

1. settings API로 즉시 보정 (예: `5`)
2. 응답의 `alarm_target_minutes` 확인
3. 필요 시 settings 파일(`./logs/settings.json`) 백업 후 재초기화

---

## 7) 모니터링/관측 최소 기준

배포 후 최소 24시간 관찰:
- `hololive-alarm` `/ready` 503 빈도
- 알림 누락/중복 신고 건수
- bot/alarm 컨테이너 재시작 횟수
- settings API 변경 이벤트 로그(`settings_update`)

---

## 8) 후속 고위험 작업(아직 미완료)

`dedup` legacy JSON fallback 제거는 별도 작업으로 진행합니다.

선행 조건:
1. writer/read 경로 정합성 확보 (Go writer 포함)
2. dry-run 가능한 마이그레이션 도구 준비
3. key snapshot + 롤백 절차 문서화
4. soak 후 fallback 제거

---

## 9) 승인 게이트

다음 조건을 모두 만족할 때만 “완료”로 처리합니다.

1. `hololive-bot`, `hololive-alarm` health/readiness 정상
2. settings 업데이트 시 `alarm_target_minutes` 즉시 반영
3. 24시간 내 restart loop/대량 알림 오류 없음
4. 롤백 절차 리허설 1회 수행
