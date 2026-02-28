# CI D3 Follow-up (2026-02-24)

## 배경

- `docs/BACKEND_GUIDE_REVIEW_20260223.md`의 D3 이슈(워크플로 path 오타 + dependency 취약점 스캔 부재) 대응.

## 적용 변경

1. `.github/workflows/scraper-rs.yml` trigger path 오타 수정
   - `scraper-rs-ci.yml` → `scraper-rs.yml`
2. CI 보안 스캔 단계 추가
   - `taiki-e/install-action@cargo-audit`
   - `cargo audit --deny warnings` (취약점/경고 시 CI 실패)

## 검증 메모

- 정적 점검: workflow 파일 내 trigger path와 실제 파일명 일치 확인
- 정적 점검: Cargo Audit step이 Test 이후, Build 이전에 존재함을 확인
