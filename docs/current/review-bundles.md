# Review Source Bundles

이 문서는 리뷰용 소스 번들 산출 규칙을 정의합니다.

## Source bundle export

- Script: `scripts/review/export-source-bundle.sh`
- Usage: `scripts/review/export-source-bundle.sh [output_dir]`
- Output: `<output_dir>/hololive-bot-source-YYYYMMDDTHHMMSSZ.tar.gz`

Default output directory: `artifacts/review`

## 제외 항목

번들 생성 시 `.worktrees`, `.tasklists`, `.runlogs`, AI/agent 실행 디렉터리(`.codex`, `.claude`, `.serena`, `.gemini`), `artifacts`, `logs`, `node_modules`, `dist`, `coverage`, 기존 tarball, `BUNDLE_MANIFEST.txt`이 제외됩니다.

## 사용 목적

- 리뷰/감사 시 `review` 번들의 스코프 오염(`.worktrees`, 빌드 산출물 등) 방지
- Docker build context 오염과 분리된 산출물 경로를 통한 재현 가능한 패키지 전달
