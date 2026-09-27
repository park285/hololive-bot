# 라이브 확인·목록 접기 운영 전환

**Decisions:** `DEC-20260926-hololive-live-absence-evidence` (governing), `DEC-20260927-live-check-slot-isolation` (governing), `DEC-20260926-hololive-list-reply-fold-default` (constraint), `DEC-20260814-hololive-youtube-three-provider-convergence-v2` (constraint)

## Execution capsule

**Goal:** 검증한 동일 코드 revision의 API·worker·collector를 중앙과 AP a/b/c/d에 반영하고 새 확인 증거의 운영 수신을 검증한다.
**Context:** 로컬 구현·전체 빌드는 통과했다. 중앙 API/worker는 6b6f99a, collector는 476a150이며 migration 217까지 적용됐다.
**Constraints:** 사용자 “필요한건 전부 승인”의 배포·migration·로컬 커밋 범위. 빌드/테스트는 kapu만, 원격은 검증 artifact/no-build만. Git publication·secret 조회/변경·unrelated runtime/데이터 변경·rollback 자료 정리는 제외한다.
**Evidence:** 사용자 승인 원문, docs/review/live-diagnostics-list-fold-20260927.md, guarded 중앙 ledger와 image metadata, collector 활성화 runbook.
**Success:** AC01~AC03/V01~V02. 실제 카카오톡 화면 증명은 유효한 지정 테스트 방이 없어 별도 차단 항목으로 보존한다.
**Output:** 검증된 배포 artifact, 중앙/AP 전환과 rollback 지점, 민감값 없는 운영 검증 문서.

## 순서와 중단 조건

통합 담당자가 순서대로 수행한다. 코드 SHA `d0f8a2feccbb47001ba7193d0df641c9d803cbbc`에는 현재 운영의 mekpark 수정도 포함한다. 중앙/Seoul은 arm64, a/d는 amd64 v1이다. 현재 collector는 새 kind 이전 버전이므로 공유-check 버전 혼합 이력은 없으며 API·migration을 먼저 전환한다.

중앙의 실제 overlay(prod/live-compat/admin-web/x-spaces 및 c의 main-ap 두 파일)를 보존한다. 현재 image ID로 rollback tag를 만들고 변경 대상 runtime 파일만 보관한다. API stop → migration 218~220 → 새 API readiness/target → worker → c → a/d/b 순서다. API 중단 동안 bot/admin 요청이 일시 실패할 수 있고 fleet 완료까지 확인 coverage는 불완전할 수 있다. 새 kind backlog가 있으면 새 decoder를 유지한다.

218은 kind 계약·두 canonical 표·grant 추가, 219는 구 reader 정지 뒤 부분 index 제거, 220은 정확히 일치하는 표준 전역 빈 문구만 교체한다. custom/override는 보존한다. 구 migrator 재실행 금지. API rollback은 새 collector 중지와 decoder backlog 처리 및 219 index 재생성이 필요한 별도 복구 경로다. 자동으로 옛 API로 되돌리지 않는다. native 스크립트의 기존 health-failure rollback은 해당 collector만 대상으로 유지한다.

예상 밖 ledger/checksum, 장기 transaction, HBA/ingress drift, 잘못된 arch/revision, Node 버전 부족, 새 target 부재, 실패/결과 불명 전환은 종속 작업을 중지한다. read-only DB guard를 매 세션 증명하고 aggregate/LIMIT만 읽는다. AP rollback tag/이전 release는 정리하지 않는다.

## 작업

### T01 Artifact와 rollback 준비

clean release revision의 전체 build-only 결과를 사용하고 배포 static contract 5개를 확인한다. 중앙용 arm64 3개 image를 kapu에서 만들고 full SHA/arch를 검사한다. 기존 runtime file/image identity와 migration checksum을 확인한 뒤 범위 제한 archive와 rollback 지점을 준비한다.

### T02 중앙 순서 전환

검증 image를 전송/load하고 원격에서 동일 image ID/arch/SHA를 확인한다. 필요한 runtime 파일만 설치한다. old API를 정지하고 migration을 성공시킨 뒤 API → worker → c를 `up --no-build --no-deps --pull never`로 전환한다. 새 decoder/target을 먼저 확인한다.

### T03 AP fleet 전환

동일 SHA로 기존 native 배포 스크립트(a/d) 및 Seoul no-build 배포 스크립트(b)를 사용한다. 원본 작업트리의 비밀 파일은 authentication에만 사용하며 복사/출력하지 않는다. 각 readiness/completion gate와 revision을 확인한다.

### T04 운영 근거 기록

change_started_at 이후 상태·연결/오류 aggregate·migration ledger·canonical 확인 수신을 기록한다. rollback 지점과 잔여 미검증 항목을 보고한다. 지정방 없는 실제 발송은 하지 않는다.

## 수용 기준

### AC01 동일 검증 코드 반영

중앙 API/worker 및 a/b/c/d collector가 검토한 SHA를 실행한다. 중앙/b image는 arm64, a/d는 amd64 v1이고 원격 빌드가 없다.

### AC02 확인 계약 운영 수신

218~220 ledger와 schema/grant가 존재하고 구 index는 없다. 새 channel_live_check target 및 새 canonical evidence 수신을 aggregate로 관측한다. 각 서비스와 fleet의 readiness가 통과한다.

### AC03 보존과 복구 가능성

사용자 원본 보호 파일 및 unrelated runtime을 건드리지 않는다. 이전 image/release와 변경 전 runtime 파일을 보존한다. custom template/override·pending 데이터의 임의 정리나 구 migrator 실행이 없다.

## 검증

### V01 로컬 artifact 검증

전체 build-only/local CI, 배포 script 5개 contract, candidate image SHA/architecture를 통과한다.

### V02 새 runtime 검증

중앙/AP completion check, change_started_at 이후 revision/StartedAt/readiness/error 경계, guarded DB ledger/target/evidence를 관측한다. 실제 카카오 UI 확인은 이 배포 gate와 혼동하지 않고 지정방 대기 항목으로 보고한다.
