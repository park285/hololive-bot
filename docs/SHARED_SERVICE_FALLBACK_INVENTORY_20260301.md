# Shared Service Fallback Inventory (1차)

작성일: 2026-03-01 (KST) / 2026-02-28 (UTC)

목적: `hololive/hololive-shared/pkg/service/**` fallback 경로를 분류하고, 제거 대상 우선순위를 명확화.

---

## 1) 분류 기준
- **필수(가용성 핵심)**: 외부 의존성 장애 시 서비스 연속성을 보장
- **조건부(품질 보완)**: 결과 품질 개선용이며 장애 시에도 핵심 플로우는 유지
- **제거 대상(legacy/중복/호환성 부채)**: 계측 후 단계적 제거 가능

---

## 2) 제거 대상 후보 (우선)

| 경로 | 심볼/라인 | 현재 fallback | 조치 |
|---|---|---|---|
| `pkg/service/majorevent/summarizer_prompt.go` | `ongoing_note` 호환 경로 | `ongoing_events` 부재 시 구 포맷 허용 | ✅ 제거 완료 (구형 `ongoing_note` 텍스트 fallback 폐기) |
| `pkg/service/youtube/scraper/videos.go` | legacy `/videos?view=0&sort=dd&shelf_id=0` 재시도 | 표준 파싱 실패 시 legacy URL 재시도 | ✅ 제거 완료 (legacy URL 재시도 경로 폐기, RSS fallback 단일화) |
| `pkg/service/youtube/scraper/channel.go` | `c4TabbedHeaderRenderer` fallback | 구 헤더 포맷 추출 경로 유지 | ✅ 제거 완료 (pageHeaderRenderer 단일 경로화) |
| `pkg/service/database/postgres.go` | `GetDB()` deprecated accessor | `GetPool()`와 이중 경로 유지 | ✅ 제거 완료 (`GetDB()` 제거, `GetPool()` 단일 경로화) |

---

## 3) 필수/조건부 대표 항목

### 필수(유지)
- Holodex 장애 시 scraper fallback (`pkg/service/holodex/service.go`)
- YouTube HTML 파싱 실패 시 RSS/대체 경로 (`pkg/service/youtube/scraper/videos.go`)
- 배치 채널 조회 실패 시 단건 조회 fallback (`pkg/service/holodex/service.go`)

### 조건부(유지 + 계측 강화)
- 번역/요약 경로 실패 시 deterministic 결과 사용 (`pkg/service/membernews/*`, `pkg/service/majorevent/*`)
- dedup lock 장애 시 noop 동작 (`pkg/service/delivery/locker.go`)
- 템플릿 렌더 실패 시 하드코딩 formatter (`pkg/service/youtube/outbox/dispatcher.go`)

---

## 4) 다음 액션
1. 제거 대상 4건(ongoing_note / legacy videos URL / c4 header / GetDB) 회귀 테스트 유지
2. 향후 신규 fallback 추가 시 분류 기준(필수/조건부/제거 대상) 선적용
3. 제거 반영사항 운영 문서/runbook 동기화
