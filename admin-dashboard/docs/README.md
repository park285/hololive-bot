# 프로젝트 문서 구조

이 디렉토리는 **전체 인프라/공통 문서**를 포함합니다. 프로젝트별 문서는 각 프로젝트의 `docs/` 폴더에 있습니다.

## 최근 진행 상황 (2026-01-02)

| 작업 | 상태 |
|:---|:---|
| Admin Backend 분리 (Phase 4a) | ✅ 완료 |
| OpenAPI Pipeline 구축 | ✅ 완료 |
| Game Bot Admin API 백엔드 구현 | ✅ 완료 |
| Gzip 미들웨어 제거 (Edge Compression) | ✅ 완료 |
| 문서 정리 및 README 추가 | ✅ 완료 |

## 현행 문서

| 문서 | 설명 | 상태 |
|:---|:---|:---|
| `openapi-pipeline.md` | OpenAPI 자동 생성 파이프라인 | Active |
| `uds-support.md` | Unix Domain Socket 지원 문서 | Active |

## 프로젝트별 문서

| 프로젝트 | 문서 경로 | 주요 내용 |
|:---|:---|:---|
| `admin-dashboard` | `admin-dashboard/README.md` | Admin Backend 설정 |
| `game-bot-go` | `game-bot-go/docs/` | Admin API, gRPC 마이그레이션 |
| `hololive-kakao-bot-go` | `hololive-kakao-bot-go/docs/` | 세션 보안, 데이터베이스 |

## 아카이브

`archive/` 폴더에는 완료되었거나 KI로 이전된 계획/운영 문서가 있습니다:
- `admin-separation-plan.md`: Admin 분리 계획 (KI로 이전됨)
- `admin-ui-operations.md`: Admin UI 운영 가이드 (KI로 이전됨)
- `admin-ui-upgrade-plan.md`: Admin UI 업그레이드 계획 (진행중, Phase 4a/5 완료)

## Knowledge Items (KI)

최신 정보는 KI 시스템에서 관리됩니다:
- `unified_admin_dashboard_architecture`: Admin Dashboard 아키텍처
- `go_engineering_patterns`: Go 엔지니어링 패턴
- `infrastructure_and_security`: 인프라 운영/보안

---

*마지막 업데이트: 2026-01-02*
