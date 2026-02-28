# Hololive Alarm Pre-deploy Checklist

기준: `docs/HOLOLIVE_ALARM_HIGH_RISK_RUNBOOK_20260226.md`

## 1) 환경 변수/설정 점검

- [ ] `ALARM_HOLODEX_API_KEYS` 설정 확인 (JSON array string)
- [ ] Twitch 비활성 운영 시 `ALARM_TWITCH_ENABLED=false` 확인
- [ ] `./logs:/app/logs` 볼륨 쓰기 가능 확인

```bash
echo "$ALARM_HOLODEX_API_KEYS"
python3 - <<'PY'
import json, os
json.loads(os.environ['ALARM_HOLODEX_API_KEYS'])
print('OK: valid JSON array string')
PY

echo "$ALARM_TWITCH_ENABLED"
test -w ./logs && echo "OK: logs writable"
```

## 2) Compose 정합성

```bash
docker compose -f docker-compose.prod.yml config -q
```

PASS 기준:
- [ ] 명령 exit code 0

## 3) 백업(필수)

- [ ] PostgreSQL dump 완료
- [ ] Valkey snapshot/RDB 보관
- [ ] `.env`, `docker-compose.prod.yml` 백업

권장 증적:
```bash
ls -lh <backup-dir>
```

## 4) 배포 실행 전 게이트

아래 모두 충족 시에만 배포 진행:
- [ ] Holodex key JSON 유효성 확인
- [ ] compose config 검증 통과
- [ ] 백업 3종 확보
- [ ] 배포 대상 이미지/태그 식별 완료
