# 운영자가 버전·통계·준비 상태 확인하기

관리 API에서 멤버·알람·방 수, 버전과 가동 시간을 읽고, 런타임별 상태와 원인을 나눠 본다.

## 하위 기능

관리 통계, process health와 기능 readiness, 인증된 상세 상태와 worker profile/metrics, 선택적 pprof, 스키마 적용과 복구 도구.

## 사용자 경로

- `GET /api/holo/stats`는 멤버 저장소와 alarm worker에서 각각 수를 얻는다. `hololive-api` bot/admin, `alarm-worker`, `youtube-collector`의 상태 노출 설정은 [토폴로지](../current/PROJECT_MAP.md)와 각 서비스 문서를 따른다.
- 공통 라우터의 `GET /health`, `/ready`, `/internal/ready`, `/metrics`, `/diagnostics/workers`; pprof 전용 listener의 `GET /debug/pprof/`, `/cmdline`, `/profile`, `/symbol`, `/trace`, `/:profile` 및 `POST /debug/pprof/symbol`(하위 경로는 모두 `/debug/pprof` 기준). 런타임/설정에 따라 일부만 노출한다.
- 세 runtime의 `--check-worker-profile`, 각 `healthcheck` CLI; `db-migrate`의 `--baseline-through`, `--statement-timeout`, `--allow-blocking-index-drop`은 기동 전 스키마 작업이다. collector의 `po-broker --version`, `--healthcheck`, `--socket` 또는 `--listen-fd`는 내부 issuer용이다. 재처리 기준점 도구는 [관측](collection.md)에 있다.
- [발송 정산](dispatch.md)의 별도 운영자 SQL 도구는 `scripts/maintenance/preview-alarm-dispatch-closeout.sql`, `record-alarm-dispatch-closeout.sql`, `lookup-alarm-dispatch-closeout.sql`이다. 기록 조치는 일반 조회와 구분한다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request GET /api/holo/stats 200 '')
jq '{status,members,alarms,rooms,version,uptime}' "$p/extra-1.json"
cat "$p/identity" "$p/database"
```

`version`은 실행 소스의 revision과, 알람 수는 구독 삭제 후의 0과 대조한다. 실제 설정·인증서는 가져오지 않는다. `database`는 임시 dbtest 마이그레이션 원장의 행 수이지 운영 원장이 아니다.

## 함정

빈 포트·`/health` 200·프로필 검사 성공만으로 제품이 준비됐다고 판정하지 않는다. pprof, 지원 여부가 불분명한 진단 경로, 운영 마이그레이션과 deploy/복구 스크립트는 자동 실행하지 않는다. 배포와 release는 이 지도의 사용자 여정이 아니다.
