<task>
배정 worktree: /home/kapu/work/iris-stack/hololive-bot-apsm-3c (브랜치 refactor/youtube-outbox-fence-convergence. 0단계 4개 수정 + sendfence 커밋(b4591aa54) 위. 모든 편집은 이 경로의 절대 경로로)
CHANGELOG 담당: 예(하위 PR마다 한 항목).

3단계 youtube-outbox 수렴. 설계는 local://apsm-design-S3B.md 전체(특히 §6 PR 분할: 3c-1 → 3c-2 → 3c-3, 3c-4는 다음 릴리스). 사용자 결정은 local://apsm-decisions.md(alarm_states.authorized_at claim 제거·동작 변화 수용).

**하위 PR 단위로 진행한다.** 이번 지시는 **3c-1만**: 행 fence (status, locked_by, row_version) + locked_by nullable 열 추가 migration + 결함 5-3 수정(BeginSending·ApplyPreparedFailure·PrepareClaimed가 호출자의 claim 버전으로 fence, BeginSending에 lease 유효 조건) + PreparationLease 삭제 + 3중 CAS의 locked_at IS NOT DISTINCT FROM·attempt_count 조건을 locked_by 비교로 대체. 미스는 sendfence.MissError로. 3c-1이 끝나면(검증 포함) 최종 응답으로 결과를 내고 멈춘다. 부모가 커밋한 뒤 같은 대화로 3c-2 지시를 보낸다.

규칙:
- migration 파일 번호는 **327**(부모 배정). manifest.txt는 현재 마지막 줄(185 324_…) 다음에 `186 327_<이름>.sql` 추가(ordinal은 통합 때 부모가 맞춤). CONVENTIONS.md를 따르고, dbtest schema golden(hololive/hololive-dbtest/testdata) 등 갱신 대상이 있으면 함께 갱신. rolling 호환(구 바이너리가 새 열을 무시해도 안전)을 확인.
- sendfence(hololive/hololive-alarm-worker/internal/egress/sendfence) 공개 API 수정 금지. 부족하면 부모에게 먼저 `write agent://Main`. fence.go 문서 주석의 SQL 관측 열 규약을 따른다(version_matches = row_version 등가, lease_valid = locked_at+lease_ms > clock).
- 논리 그룹·ledger·commit 응답 adjudication·frozen request는 보존한다.
- 시험: 설계 §6 3c-1 목록, 특히 '같은 토큰의 계약이 경로마다 다르던' 5-3 재현(stale claim 버전으로 BeginSending 거절)을 수정 전 실패·수정 후 통과로 확인. 기존 youtubedispatch 시험 전부 통과.
- 검증: golangci-lint(alarm-worker 및 바꾼 모듈 0 issues), go test -race 영향 패키지, 관련 dbtest, check-sql-ownership, migration manifest 검사 스크립트(scripts 아래 check-migration-manifest.sh 등) 실행.
</task>
<constraints>
- 공통 규칙 local://apsm-rules.md 를 먼저 끝까지 읽고 전부 따른다.
- 다른 에이전트 위임 금지.
- 최종 응답 JSON: status, summary, design_decision, files_changed, tests, verification[{command,result}], contract_changes, risks, blockers, commit_message(`refactor(worker): 한국어 요약` + 본문).
</constraints>
