# Hololive Alarm 고위험 Runbook 실행 준비서 (Execution Prep)

- 작성일: 2026-02-26
- 기준 문서: `docs/HOLOLIVE_ALARM_HIGH_RISK_RUNBOOK_20260226.md`
- 목적: 실제 작업 직전에 사용할 **실행 체크시트**를 사전 준비

---

## 1) Pre-deploy checks (배포 전)

| Check | Command / Action | Expected | Evidence (기록) |
|---|---|---|---|
| Compose 정합성 | `docker compose -f docker-compose.prod.yml config -q` | exit code 0 | ☐ |
| Holodex key JSON 유효성 | `python3 - <<'PY'\nimport json,os\njson.loads(os.environ['ALARM_HOLODEX_API_KEYS'])\nprint('OK')\nPY` | `OK` 출력 | ☐ |
| Twitch 비활성 값 확인(해당 시) | `echo "$ALARM_TWITCH_ENABLED"` | `false` 또는 의도값 | ☐ |
| settings volume 쓰기 가능 | `test -w ./logs && echo OK` | `OK` 출력 | ☐ |
| 백업 확보 | PostgreSQL dump/Valkey snapshot/.env+compose 백업 파일 확인 | 파일 3종 이상 확인 | ☐ |

### Pre-deploy Gate (모두 충족 필요)

- [ ] Compose check 통과
- [ ] Holodex key JSON 파싱 성공
- [ ] 백업 확보 확인
- [ ] 배포 대상 이미지 태그 기록

---

## 2) Post-deploy validation (배포 직후)

## 2.1 0~10분 기본 검증

| Check | Command | Expected | Evidence |
|---|---|---|---|
| bot health | `curl -fsS http://127.0.0.1:30001/health` | 200 | ☐ |
| alarm health | `curl -fsS http://127.0.0.1:30011/health` | 200 | ☐ |
| alarm ready | `curl -fsS http://127.0.0.1:30011/ready` | 200 + degraded 아님 | ☐ |
| bot 에러 로그 | `docker logs --tail=200 hololive-kakao-bot-go` | persist pool 오류 없음 | ☐ |
| alarm 에러 로그 | `docker logs --tail=200 hololive-alarm` | Holodex 파싱 오류 없음 | ☐ |

## 2.2 settings 반영 검증

```bash
curl -sS -X POST http://127.0.0.1:30001/api/holo/settings \
  -H 'Content-Type: application/json' \
  -H 'X-API-Key: <API_SECRET_KEY>' \
  -d '{"alarmAdvanceMinutes":7}'
```

기대값:

- `runtime.alarm_applied == true`
- `runtime.alarm_target_minutes == [7,3,1]`

복구:

- 검증 후 운영 표준값으로 즉시 재설정 (예: `5`)

---

## 3) 운영 기록 템플릿

- 배포 시각:
- 담당자:
- 배포 대상 이미지 태그:
- Pre-deploy gate 결과: PASS / FAIL
- Post-deploy validation 결과: PASS / FAIL
- 장애/이슈 요약:
- 후속 액션:
