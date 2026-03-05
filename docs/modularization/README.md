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

## 진행 규칙

- task 완료 시 `TODO.md`에서 `[ ]` -> `[x]` 체크
- 진행중 task는 `[~]` 표기
- Phase별 독립 PR, 기능/구조 변경 혼합 금지

## Phase 11 상태 스냅샷 (2026-03-05)

- 완료(코드 반영): **A1/A2/A3, B1/B2/B3, C1/C2/C3/C4, D3, E2/E3/E4**
- 진행중: **B8, D1, D4, D5, E1**
- 미착수/후속: **B4/B5/B6/B7/B9, D2**

상세 체크는 `TODO_PHASE11.md`를 기준으로 유지합니다.
