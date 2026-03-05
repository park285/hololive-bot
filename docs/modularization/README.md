# Modularization

hololive-bot 전체 모듈 분할화 작업 문서.

## 문서 목록

| 파일 | 용도 |
|------|------|
| `PLAN.md` | Phase 0~6 실행 계획 (shared 분리 중심: 의존성 매트릭스, 목표 구조, 성공 지표) |
| `PLAN_EXTENDED.md` | Phase 7~10 확장 계획 (서비스 내부 분할, 인터페이스화, 모듈 재배치, Rust 세분화) |
| `TODO.md` | Phase 0~6 상세 task 체크리스트 |
| `TODO_EXTENDED.md` | Phase 7~10 상세 task 체크리스트 + 전체 병렬 실행 가이드 + 충돌 회피 규칙 |
| `KICKOFF_20260303.md` | 착수 스냅샷 (사전 검증 결과, 첫 실행 순서, 리스크 메모) |
| `PLAN_PHASE11.md` | Phase 11 품질 강화 계획 (보안, 중복 제거, I/O 성능, 테스트, 구조 보강) |
| `TODO_PHASE11.md` | Phase 11 상세 task 체크리스트 (5 트랙 병렬 실행 가이드 포함) |
| `REVIEW_PHASE11_7_D1_D2_D4.md` | phase11-7 병렬 실행(D1/D2/D4) 코드 품질/검증 리뷰 리포트 |

## 진행 규칙

- task 완료 시 `TODO.md`에서 `[ ]` -> `[x]` 체크
- 진행중 task는 `[~]` 표기
- Phase별 독립 PR, 기능/구조 변경 혼합 금지

## Phase 11 상태 스냅샷 (2026-03-05)

- 완료(코드 반영): **A1/A2/A3, B1/B2/B3/B4/B5/B6/B9, C1/C2/C3/C4, D3/D5, E1/E2/E3/E4**
- 진행중: **B7/B8, D1/D2/D4**
- 미착수/후속: **전체 모듈 검증 정리(go test/lint baseline)**

상세 체크는 `TODO_PHASE11.md`를 기준으로 유지합니다.

### phase11-7 반영 점검 (2026-03-05)

- `REVIEW_PHASE11_7_D1_D2_D4.md` 기준 수치 반영 확인:
  - D1 checker coverage **82.5%**
  - D2 notification coverage **76.6%**
  - D4 stream-ingester internal/app coverage **60.8%**
- phase11-7에서 완료된 항목(B9 포함)과 미완료 항목(D1/D2/D4 잔여 TODO)을 `TODO_PHASE11.md`와 동기화

### phase11-8 (D5/E1) 검증 스냅샷 (2026-03-05)

- D5: `go test -count=1 -cover ./pkg/service/youtube/scraper/...` **61.7% (PASS)**
- D5 회귀: malformed JSON 혼합 케이스 방어 로직 반영 후 table-driven 회귀 통과
- E1: `config_db.go/config_cache.go/config_iris.go/config_notification.go/config_telemetry.go/config_kakao.go/config_llm.go` 분할 반영
- lint: `./pkg/config` PASS, `./pkg/service/youtube/scraper/... --new` PASS
- 참고: 전체 모듈 타입체크/lint는 타 트랙 baseline 이슈로 별도 정리 필요

### phase11-13 (B8/D1/D4) 동기화 스냅샷 (2026-03-05)

- D1: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/alarm/checker/...` **82.7% (PASS)**
- D4: `cd hololive/hololive-stream-ingester && go test -count=1 -cover ./internal/app/...` **62.0% (PASS)**
- B8: `cd hololive/hololive-shared && go test -count=1 -cover ./pkg/service/cache/...` **42.0% (PASS)**
- 잔여 핵심:
  - B8 `hololive-shared/pkg/service/cache/service_test.go` miniredis 공통 헬퍼 치환
  - D1 youtube checker table-driven 5개 시나리오
  - D4 runtime_builder 정상 빌드(모든 의존성 제공) 시나리오
