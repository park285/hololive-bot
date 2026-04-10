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

## Full review bundle export

- Script: `scripts/review/export-full-bundle.sh`
- Usage: `scripts/review/export-full-bundle.sh [output_dir]`
- Optional untracked inclusion:
  `INCLUDE_UNTRACKED=true scripts/review/export-full-bundle.sh [output_dir]`
- Output: `<output_dir>/hololive-bot-review-bundle-full-YYYYMMDDTHHMMSSZ.tar.gz`
- Default policy: tracked files only
- Full bundle must always include `BUNDLE_MANIFEST.txt`

### Full bundle policy

- 기본값은 tracked file only 입니다.
- `.worktrees`, `.tasklists`, agent 실행 디렉터리, `logs`, `artifacts`, 기존 tarball 같은 로컬 오염물은 기본적으로 포함되지 않습니다.
- untracked 파일이 정말 필요할 때만 `INCLUDE_UNTRACKED=true`를 명시합니다.
