# Hololive Alarm Post-deploy Validation

기준: `docs/HOLOLIVE_ALARM_HIGH_RISK_RUNBOOK_20260226.md`

## 1) 배포 직후 상태 확인

```bash
docker compose -f docker-compose.prod.yml ps hololive-bot hololive-alarm
docker logs --tail=200 hololive-kakao-bot-go
docker logs --tail=200 hololive-alarm
```

PASS 기준:
- [ ] bot에서 `failed to create alarm service`, `persist pool` 관련 치명 오류 없음
- [ ] alarm에서 Holodex key 파싱/설정 오류 없음

## 2) 헬스체크

```bash
curl -fsS http://127.0.0.1:30001/health
curl -fsS http://127.0.0.1:30011/health
curl -fsS http://127.0.0.1:30011/ready
```

PASS 기준:
- [ ] `/health` 2개 모두 HTTP 200
- [ ] `/ready` HTTP 200 (degraded 아님)

## 3) settings 반영 검증 (`alarmAdvanceMinutes`)

```bash
curl -sS -X POST http://127.0.0.1:30001/api/holo/settings \
  -H 'Content-Type: application/json' \
  -H 'X-API-Key: <API_SECRET_KEY>' \
  -d '{"alarmAdvanceMinutes":7}'
```

PASS 기준:
- [ ] 응답 `runtime.alarm_applied == true`
- [ ] 응답 `runtime.alarm_target_minutes == [7,3,1]`

운영 원복:
- [ ] 검증 종료 후 표준값(예: `5`)으로 재설정

## 4) Twitch disabled readiness 확인

전제: `ALARM_TWITCH_ENABLED=false`

PASS 기준:
- [ ] alarm 로그에서 Twitch disabled 상태 확인
- [ ] `/ready`가 `scheduler_healthy=true`로 유지
