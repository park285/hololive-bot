# LLM Scheduler 운영 Runbook

> 마지막 업데이트: 2026-02-28
> 대상 서비스: `llm-scheduler` (포트 `30003`), `hololive-bot` 운영 API (포트 `30001`)

---

## 1) 목적

P5 분리 이후 `llm-scheduler` 장애/재실행/수동 실행 절차를 표준화합니다.

다루는 기능:
- Major Event 주간/월간 알림
- Member News 주간 다이제스트 즉시 실행

---

## 2) 주요 엔드포인트

### 외부(운영자) 호출
- `POST /api/holo/majorevent/trigger` (주간 알림)
- `POST /api/holo/majorevent/monthly-trigger` (월간 알림)
- `POST /api/holo/settings/llm` (membernews 즉시 실행)

### 내부(서비스 간) 호출
- `POST /internal/trigger/majorevent-weekly`
- `POST /internal/trigger/majorevent-monthly`
- `POST /internal/trigger/membernews-weekly`
- `SUBSCRIBE config:update` (`membernews_weekly_run_now`)

> 보안 정책(2026-03-01 반영): `API_SECRET_KEY`가 설정된 환경에서는 내부 트리거 호출도 `X-API-Key` 헤더가 필수입니다.  
> admin-api trigger proxy는 동일한 `API_SECRET_KEY`를 내부 호출에 자동 전달합니다.

---

## 3) 정상 상태 점검

```bash
docker compose -f docker-compose.prod.yml ps llm-scheduler hololive-bot
curl -fsS http://127.0.0.1:30003/health
curl -fsS http://127.0.0.1:30001/health
docker logs --tail 150 hololive-llm-scheduler
docker logs --tail 150 hololive-kakao-bot-go
```

정상 기준:
- `llm-scheduler` 컨테이너 `healthy`
- `/health` 200 응답
- 로그에 panic/반복 실패 없음

---

## 4) 장애 대응 절차

### A. `llm-scheduler` 헬스체크 실패
1. 최근 로그 확인
2. `llm-scheduler` 단독 재기동
3. 헬스체크 및 수동 트리거 재검증

```bash
docker logs --tail 300 hololive-llm-scheduler
docker compose -f docker-compose.prod.yml up -d --build llm-scheduler
docker compose -f docker-compose.prod.yml ps llm-scheduler
curl -fsS http://127.0.0.1:30003/health
```

### B. `hololive-bot` 운영 API 트리거 호출 실패(5xx/timeout)
1. `hololive-bot` 로그에서 upstream(`llm-scheduler`) 오류 확인
2. `llm-scheduler` 헬스체크 선확인
3. 원인 제거 후 **운영자가 수동으로 1회 재실행**

### C. `409 Conflict` 응답
- 의미: 이미 동일 작업 실행 중
- 조치: 중복 실행 금지, 기존 작업 완료 후 상태 재확인

---

## 5) 수동 실행 절차

아래 호출은 모두 `X-API-Key` 필요:

```bash
export API_KEY="REDACTED"
```

### 5.1 Major Event 주간 알림 실행

```bash
curl -sS -X POST "http://127.0.0.1:30001/api/holo/majorevent/trigger" \
  -H "X-API-Key: ${API_KEY}" \
  -H "Content-Type: application/json"
```

### 5.2 Major Event 월간 알림 실행

```bash
curl -sS -X POST "http://127.0.0.1:30001/api/holo/majorevent/monthly-trigger" \
  -H "X-API-Key: ${API_KEY}" \
  -H "Content-Type: application/json"
```

### 5.3 Member News 주간 다이제스트 즉시 실행

```bash
curl -sS -X POST "http://127.0.0.1:30001/api/holo/settings/llm" \
  -H "X-API-Key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"memberNewsWeeklyRunNow":true}'
```

> 참고: Major Event 스크래핑 실행/스케줄 제어는 Rust(`hololive-rs`) 소유이며, 본 API에서 더 이상 지원하지 않습니다.

---

## 6) 실행 후 검증 체크리스트

1. API 응답 `status=ok` 확인
2. `hololive-kakao-bot-go`, `hololive-llm-scheduler` 로그에서 성공 메시지 확인
3. `409/5xx` 재발 여부 확인

```bash
docker logs --since 5m hololive-kakao-bot-go
docker logs --since 5m hololive-llm-scheduler
```

---

## 7) 공통 주의사항

- 자동 재시도/무한 재시도 금지
- 동일 트리거 중복 호출 금지 (409 처리)
- 운영 중 수동 실행은 1회 단위로 관찰 후 진행
