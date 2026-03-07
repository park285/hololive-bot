# Hololive Bot 문서

## 모듈 경로 정리 (Phase 3 완료)

- `hololive-kakao-bot-go` : bot 전용
- `hololive-admin` : admin-api
- `hololive-llm-sched` : llm-scheduler
- `hololive-stream-ingester` : stream-ingester
- `hololive-shared` : 공통 패키지(domain/service/providers/server)

## API 문서

| 문서 | 설명 |
|:---|:---|
| [api/session_security.md](api/session_security.md) | 세션 보안 가이드 |
| [api/milestone_api.md](api/milestone_api.md) | 마일스톤 API 명세 |

## 시스템 문서

| 문서 | 설명 |
|:---|:---|
| [database.md](database.md) | 데이터베이스 스키마 및 설정 |
| [LLM_SCHEDULER_RUNBOOK.md](LLM_SCHEDULER_RUNBOOK.md) | llm-scheduler 장애/재실행/수동 트리거 운영 절차 |
| [DISTRIBUTED_RATE_LIMITING.md](DISTRIBUTED_RATE_LIMITING.md) | Holodex/YouTube scraper 분산 레이트 리미터 설계/운영 가이드 |
| [STREAM_INGESTER_RUNBOOK.md](STREAM_INGESTER_RUNBOOK.md) | stream-ingester 단독 ingestion 운영/장애 대응 절차 |
| [P6_REMAINING_TASKS.md](P6_REMAINING_TASKS.md) | P6 종료 상태와 후속 운영 메모 |

## 로컬 기능 문서

| 문서 | 설명 |
|:---|:---|
| [local/alarm-notification.md](local/alarm-notification.md) | 알람 알림 시스템 |
| [local/hololive-feature.md](local/hololive-feature.md) | 홀로라이브 기능 설명 |

## 관련 KI

최신 아키텍처/패턴 정보는 Knowledge Items에서 관리됩니다:
- `unified_admin_dashboard_architecture`: Admin Dashboard 아키텍처

---

*마지막 업데이트: 2026-03-07*
