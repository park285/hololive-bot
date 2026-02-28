# P6 남은 작업 목록 (Stream Ingester 분리)

> 마지막 업데이트: 2026-02-27
> 범위: `hololive-kakao-bot-go` P6 마감 전까지 남은 실행 항목

---

## 현재 상태 요약

- 완료:
  - `stream-ingester` 바이너리/런타임 1차 분리
  - `BOT_INGESTION_ENABLED` 토글 반영
  - Holodex/YouTube scraper 분산 rate limiter 적용
  - 기본 Runbook/설계 문서 작성
  - ingestion 분산 락 가드 추가 (`lock:ingestion:runtime`, SetNX + compare-and-expire renew + release)
  - ingestion 락 이벤트 운영 로그 규칙 반영 (`ingestion_lease_*`, `bot_ingestion_enabled`)
- 미완료:
  - 없음 (운영 검증/롤백 리허설은 본 단계에서 제외)

## P6 종료 상태

- 상태: `✅ 완료` (2026-02-27)
- 비고: 운영 검증/롤백 리허설은 운영자 결정으로 필수 범위에서 제외

---

## 후속 권장 항목 (선택)

## 1) 운영 컷오버 검증 (제외됨)

- 목표:
  - `hololive-bot`는 ingestion 비활성
  - `stream-ingester`만 ingestion 활성
- 확인 항목:
  - 스케줄러/아웃박스/photo sync가 `stream-ingester`에서만 동작
  - bot에서 ingestion 관련 로그가 비활성 상태로 유지
- 상태:
  - 운영자 결정으로 P6 종료 조건에서 제외 (2026-02-27)

## 2) 중복 실행 가드 보강 (완료)

- 목표:
  - 설정 실수(`BOT_INGESTION_ENABLED` 동시 true) 시 즉시 탐지 가능
- 작업:
  - 부팅 시 설정 검증/경고 로그 강화
  - 운영 체크리스트에 토글 검증 단계 명시
- 현재 반영:
  - bot/stream-ingester ingestion 시작 시 Valkey 분산 락 획득 강제
  - 락 미획득 시 프로세스 시작 실패(명시적 에러)
  - 런타임 renew loop + shutdown release 적용
- 남은 작업:
  - (없음) 완료

## 3) stream-ingester 메트릭/대시보드 분리 (권장)

- 목표:
  - ingestion SLA를 bot과 분리 관측
- 작업:
  - 서비스별 지표/로그 태그 정리
  - 장애 기준(지연/실패율) 임계치 문서화
- 완료 기준:
  - stream-ingester 전용 모니터링 기준 수립

## 4) 배포/롤백 절차 확정 (권장)

- 목표:
  - 장애 시 빠른 롤백 경로 확보
- 작업:
  - [x] `stream-ingester` 단독 재기동 절차
  - [x] bot ingestion 재활성화 임시 우회 절차(비상 시)
  - [ ] (선택) 실제 운영 리허설(롤백 1회) 결과 기록
- 완료 기준:
  - Runbook에 단계별 명령/판단 기준 포함

## 5) P6 종료 선언 조건 충족 (완료)

- 목표:
  - P6 상태를 `✅ 완료`로 승격
- 조건:
  - [x] 24h 운영 검증 요구사항 제외 (운영자 결정, 2026-02-27)
  - [x] 중복 실행 가드/문서 반영 완료
  - [x] 운영 모니터링 기준(락 이벤트) 초안 확정
  - [x] 롤백 리허설 요구사항 제외 (운영자 결정, 2026-02-27)

---

## 검증 명령 (기본)

```bash
go test ./internal/service/ratelimit ./internal/service/holodex ./internal/service/youtube/scraper ./internal/app
go build ./...
docker compose -f docker-compose.prod.yml ps hololive-bot stream-ingester
```
