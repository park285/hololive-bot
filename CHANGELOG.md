# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **평문 서식 모사 퇴역:** `shared-go`를 v2.26.1로 올렸습니다. 일반채팅과 Markdown을 끈 방에 보내는 평문이 강조를 수학용 Unicode 글꼴(`𝗵𝗲𝗹𝗹𝗼`)로, 제목을 `【…】`로 바꾸지 않고 원래 문자를 씁니다. 목록·인용·표 구조와 URL, 코드 공백은 유지합니다. 알림 발송은 렌더링한 본문을 고정한 뒤 재시도에 그대로 쓰므로 배포 전 drain이 필요 없습니다. Fallback delta: none.

## r1.2.14 - 2026-11-26

- **일정 포스터 모델 진단 문구:** 빈 응답의 진단 문구(로그와 오류)에 공급자의 `blockReasonMessage`·`finishMessage` 원문을 넣지 않고 차단·종료 사유 enum만 남깁니다. 기간 읽기를 Vertex로 옮기면서 이 원문이 기간 읽기 오류에 들어가던 문제입니다. 행 추출·게시물 대조도 같은 문구를 씁니다. 문구의 thinking·출력 token 수는 continuation hop 전체를 더한 값입니다. Fallback delta: none.

## r1.2.13 - 2026-11-26

- **YouTube 원본 보관 정리의 보호 판정 지연:** 보존 대상 판정(`source_observation_retention_protected_ids`)의 "세션의 최신 적용 원본" 조건을 PostgreSQL이 해시 집합으로 바꿔, 요청 1,000건마다 LIVE·UPCOMING 세션 전체의 적용 기록(운영 약 42만 행)을 훑었습니다. 정리 한 번이 최대 9.9초 걸려 10초 예산을 넘기면 `SQLSTATE 57014`로 취소됐고, DB slot 하나를 그동안 붙잡아 같은 slot을 기다리던 원본 소비가 `acquire DB slot: context deadline exceeded`로 실패했습니다. migration 008이 이 조건을 요청 id에서 출발하도록 바꿉니다. 운영 데이터에서 판정 결과는 같고, 오래된 원본 1,000건 판정이 4.8초에서 12ms로 줄었습니다. 대기 migration이므로 `docs/current/runbooks/release.md`의 "대기 migration 적용 창" 절차로 배포합니다.
- **일정 포스터 모델 호출 SSE 전환:** hololive-api 일정 포스터의 Gemini 호출 네 곳(행 추출·번역·게시물 대조·기간 읽기)이 `generateContent` 대신 `streamGenerateContent`(SSE)를 씁니다. 청크의 parts를 이어 붙이고 마지막 사용량·종료 사유로 하나의 응답을 만든 뒤 기존 해석을 그대로 적용합니다. `CONTINUATION` 종료는 이전 단일 호출처럼 continuation token으로 다음 요청을 보내 이어 받고, 사용량은 요청마다 더합니다. SDK 자동 continuation은 꺼서 중간에 끊긴 스트림을 checkpoint에서 다시 요청하지 않습니다. 직전 요청과 같은 continuation token이 다시 오면 더 요청하지 않고 미완료 오류로 끝냅니다. 종료 사유 없이 끝나거나 후보도 차단 사유도 없이 끝난 스트림은 형식 오류가 아니라 재시도 가능한 미완료 오류(`errIncompleteStream`)입니다. 일부 내용을 보낸 뒤 `SAFETY` 같은 사유로 끝난 응답은 잘린 내용 대신 그 종료 사유로 실패합니다. 스트림이 실패해도 그때까지 받은 사용량을 기록합니다. Decisions API(`/v1beta/decisions`)는 스트리밍 모드가 없어 그대로입니다. Fallback delta: none.
- **일정 포스터 기간 읽기 Vertex 이전:** 자동 검토의 기간 읽기(`gemini-4-hydrogen`)가 Gemini Developer API 키 대신 일정표 행 추출·번역과 같은 Vertex client(ADC)를 씁니다. `SCHEDULE_POSTER_GEMINI_API_KEY_FILE`은 Vertex에 없는 Decisions API(후보 판정·변경 글 판별)에만 쓰며 설정과 mount는 바뀌지 않습니다. 기간 읽기 실패의 분류도 Vertex 기준으로 바뀌어, DNS·연결 거절·TLS·ADC 토큰 실패와 `FAILED_PRECONDITION`, 401·403·404·429·503은 자동 검토 불가로, 그 밖의 400은 후보 문제로 봅니다. Fallback delta: none.

## r1.2.12 - 2026-11-26

- **종료 live head 일회성 복구 퇴역:** 2026-09-28에 승인 범위 24건을 적용해 끝난 과거 LIVE head 대조 복구(`scripts/maintenance/preview-terminal-live-heads.sql`·`reconcile-terminal-live-heads.sql`, `scripts/runtime/test-terminal-live-head-repair.sh`, runbook)를 지웠습니다. 대상 조건이 2026-08-17 이전 head로 고정돼 다시 쓸 일이 없습니다.
- **PostgreSQL 19의 기존 행 반환 활용:** replay epoch 활성화와 YouTube 발송 요청 동결에 `ON CONFLICT DO SELECT ... RETURNING`을 적용했습니다. replay epoch는 중복 활성화 시, 발송 요청은 신규·중복 요청 모두에서 별도 재조회 한 번을 줄입니다. 최초 활성화 여부는 `OLD.singleton IS NULL`로 구분하고 기존 cutoff·작업자·사유를 보존합니다. 발송 요청은 저장된 본문 해시·경로·수신방·멤버·중복 제거 키의 검증과 재발급 세대를 유지합니다. 스키마·권한·행 잠금 강도는 바꾸지 않으며 PostgreSQL 19가 필요합니다. 적용 범위와 후속 관측 데이터 최적화 조건은 [설계 문서](docs/current/architecture/postgres19-get-or-create-20261126.md)에 정리했습니다.
- **AP 전환 owner의 신호 처리:** owner가 worker pid를 기록하는 사이에 신호가 도착하면, 이어서 시작한 차단형 `wait`이 멈춘 worker를 끝없이 기다릴 수 있었습니다. 이제 신호 상태와 worker 생존을 0.1초 간격으로 함께 확인하며 기다립니다. 신호가 온 뒤 worker 종료를 기다리는 시간도 5초에서 30초로 늘렸습니다. 끝나 가는 `systemctl restart`나 부하로 늦어진 종료가 `outcome_unknown`으로 잘못 남지 않게 하기 위해서입니다. 30초 뒤에도 남은 worker는 이전과 같이 guard를 보존한 채 `outcome_unknown`으로 남습니다.

