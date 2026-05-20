# P6 종료 메모 (YouTube Producer 분리)

> 마지막 업데이트: 2026-03-07
> 범위: `hololive-kakao-bot-go` P6 종료 상태와 후속 운영 메모

---

## 현재 상태 요약

- 완료:
  - `youtube-producer` 바이너리/런타임 1차 분리
  - bot ingestion 코드 제거, youtube-producer 단독 운영 고정
  - Holodex/YouTube producer 분산 rate limiter 적용
  - 기본 Runbook/설계 문서 작성
  - ingestion 분산 락 가드 추가 (`lock:ingestion:runtime`, SetNX + compare-and-expire renew + release)
  - ingestion 락 이벤트 운영 로그 규칙 반영 (`ingestion_lease_*`, `stream_ingestion_enabled`)
- 미완료:
  - 없음 (운영 검증/롤백 리허설은 본 단계에서 제외)

## P6 종료 상태

- 상태: `✅ 완료` (2026-02-27)
- 비고: 운영 검증/롤백 리허설은 운영자 결정으로 필수 범위에서 제외

---

## 후속 운영 항목 (선택)

## 1) 운영 컷오버 검증 (참고)

- 목표:
  - `hololive-bot`는 ingestion 코드 미포함
  - `youtube-producer`만 ingestion 활성
- 확인 항목:
  - 스케줄러/아웃박스/photo sync가 `youtube-producer`에서만 동작
  - bot에서 ingestion 시작 로그가 없어야 함
- 상태:
  - 운영자 결정으로 P6 종료 조건에서 제외 (2026-02-27)

## 2) 중복 실행 가드 보강 (완료)

- 목표:
  - ingestion ownership이 `youtube-producer`에만 고정되도록 보장
- 작업:
  - bot ingestion 코드 제거
  - 운영 체크리스트에서 bot 우회/토글 검증 제거
- 현재 반영:
  - youtube-producer 시작 시 Valkey 분산 락 획득 강제
  - 락 미획득 시 프로세스 시작 실패(명시적 에러)
  - 런타임 renew loop + shutdown release 적용
- 남은 작업:
  - (없음) 완료

## 3) youtube-producer 메트릭/대시보드 분리 (권장)

- 목표:
  - ingestion SLA를 bot과 분리 관측
- 작업:
  - 서비스별 지표/로그 태그 정리
  - 장애 기준(지연/실패율) 임계치 문서화
- 완료 기준:
  - youtube-producer 전용 모니터링 기준 수립

## 4) 배포/롤백 절차 확정 (권장)

- 목표:
  - 장애 시 youtube-producer 복구 절차 확정
- 작업:
  - [x] `youtube-producer` 단독 재기동 절차
  - [x] bot 측 ingestion 우회 절차 제거
  - [ ] (선택) 실제 운영 리허설(재기동 1회) 결과 기록
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
docker compose -f docker-compose.prod.yml ps youtube-producer
```
