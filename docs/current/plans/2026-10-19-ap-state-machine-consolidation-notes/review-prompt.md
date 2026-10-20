<task>
독립 코드 리뷰(읽기 전용). 대상 worktree와 비교 기준은 개별 task에 있다. `git -C <wt> diff <base>` (미커밋 포함: `git -C <wt> diff <base>` + `git -C <wt> status --short`)로 변경을 본다.

확인 순서:
1. 설계 문서(task에 지정)와 사용자 결정 local://apsm-decisions.md 대비 누락·이탈.
2. 정확성: fence 조건(상태·소유자·버전·lease) 누락으로 소유하지 않은 행을 바꾸는 경로, 시간 원천 혼용(Go now vs DB clock), 트랜잭션 안 외부 호출, 재시도 분류 누락, 교착 순서, migration 안전성(CONVENTIONS.md, lock_timeout, CONCURRENTLY는 트랜잭션 밖), 롤링 배포 중 구·신 버전 공존.
3. 시험 품질: 소비자 관점 동작·경계·전이·오류를 잡는가. 배선/모의 반향/소스 텍스트/개수만 세는 시험, tautology는 지적.
4. 저장소 규칙: masked slog, fmt.Errorf("action: context: %w"), ctx first, 한국어 주석(현재 계약·부수효과·비자명 이유만, 변경 이력 금지), nolint 금지, 문서·CHANGELOG 갱신.
5. 삭제돼야 할 죽은 코드·호환 shim·별칭이 남았는가.

편집 금지. 시험·빌드를 직접 돌려도 되지만(읽기 전용 명령) 파일을 쓰지 않는다. 경로는 모두 절대 경로로 읽는다(상대 경로는 메인 체크아웃을 가리킨다).
</task>
<output>
최종 응답: 한국어. 결론 한 줄(blocker 유무) → findings 목록 [{severity: blocker|major|minor|nit, file:line, 문제, 근거(코드 인용), 제안}] → 확인했지만 문제없는 항목 요약 → 미확인 범위.
</output>
