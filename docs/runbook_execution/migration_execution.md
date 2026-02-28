# Hololive Alarm Migration Execution

기준: `docs/HOLOLIVE_ALARM_HIGH_RISK_RUNBOOK_20260226.md`

## 1) 사전 반영 항목

- [ ] `ALARM_HOLODEX_API_KEYS='["key-a","key-b"]'` 설정
- [ ] `./logs` 경로 쓰기 가능 확인 (settings 파일 저장용)

```bash
echo "$ALARM_HOLODEX_API_KEYS"
python3 - <<'PY'
import json, os
json.loads(os.environ['ALARM_HOLODEX_API_KEYS'])
print('OK: valid JSON')
PY
test -w ./logs && echo "OK: logs writable"
docker compose -f docker-compose.prod.yml config -q
```

## 2) 마이그레이션(배포) 실행

```bash
docker compose -f docker-compose.prod.yml up -d --build hololive-bot hololive-alarm
```

## 3) 적용 검증

```bash
docker compose -f docker-compose.prod.yml ps hololive-bot hololive-alarm
curl -fsS http://127.0.0.1:30001/health
curl -fsS http://127.0.0.1:30011/health
curl -fsS http://127.0.0.1:30011/ready
```

## 4) settings 반영 확인

```bash
curl -sS -X POST http://127.0.0.1:30001/api/holo/settings \
  -H 'Content-Type: application/json' \
  -H 'X-API-Key: <API_SECRET_KEY>' \
  -d '{"alarmAdvanceMinutes":7}'
```

PASS 기준:
- [ ] `runtime.alarm_applied == true`
- [ ] `runtime.alarm_target_minutes == [7,3,1]`
- [ ] 설정 반영 후 표준값(예: 5)으로 복원
