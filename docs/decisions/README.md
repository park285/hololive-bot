# hololive-bot 결정 카탈로그

이 디렉터리는 `hololive-bot` 하나에만 해당하는 결정 레코드(`records/*.json`)와 생성 색인 `INDEX.md`, 이 저장소 `docs/` 제목에 대한 `inventory.allowlist`를 둡니다. 둘 이상의 저장소에 걸치는 결정은 iris-stack 메타 저장소의 `docs/agent-workflows/decisions/`에 있습니다.

이 카탈로그는 기존 결정과 당시 근거를 보존한 기록입니다. 검증·색인 생성 도구는 퇴역했으므로 새 작업에 DEC 등록이나 색인 갱신을 요구하지 않으며, `INDEX.md`는 마지막 생성 시점의 참조 자료입니다. 기록을 읽는 기준은 iris-stack의 `docs/agent-workflows/decisions/README.md`를 따릅니다. `sources`·`evidence`·`implementation`의 상대 경로는 이 저장소 루트 기준입니다. 다른 저장소의 파일은 `<repo>@<sha>:<path>` 형식으로 리비전을 고정해 가리킵니다.
