# 변경 이력

현재 app 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `v<version>` 형식을
사용합니다. 버전 관리 절차는 [릴리즈 runbook](docs/current/runbooks/release.md)을 따릅니다.
기존 `backup-before-footer-cleanup`은 backup 기준점이고 `shared-go/v0.0.1`,
`shared-go/v0.0.2`는 과거 nested module tag입니다. `[날짜-SHA]` 항목은 버전 관리 도입
전의 실제 commit 기준점이며 app version이나 새 tag를 뜻하지 않습니다.

## 미출시

## v6.0.2 - 2026-09-28

- `po-broker` HTTP `IdleTimeout`을 2초에서 30초로 늘립니다. helper Agent는 유휴 socket을 계속 약 1초에 닫습니다. v6.0.1에서는 Node event loop가 1초 넘게 멈추면 서버가 이미 닫은 유휴 socket을 helper가 재사용해 EPIPE가 `broker_unavailable`로 끝나고 발급 세션을 버렸습니다(v6.0.0에는 없던 실패). 이제 수 초의 stall에도 client가 먼저 닫습니다. 비용은 유휴 연결 최대 1개이고, 퇴역 때의 `server.Close`는 유휴 연결도 즉시 닫습니다. 이전 2초를 넘긴 유휴 연결을 재사용하는 회귀 테스트를 추가했습니다.
- `po-broker --healthcheck`(Docker HEALTHCHECK)가 keep-alive를 끄지 않습니다. 이전에는 `Connection: close` 때문에 서버가 응답 직후 먼저 닫았습니다. 이제 client가 body를 읽고 먼저 닫으며 서버는 EOF를 본 뒤 닫습니다(strace로 순서 확인).
- `po-broker`는 기동 때 SIGINT를 상속 무시(SIG_IGN)했으면 SIGINT를 감시하지 않습니다(Go runtime은 상속 SIG_IGN을 SIGINT·SIGHUP에만 존중하므로 SIGTERM은 항상 감시·종료합니다). signal을 처리할 때는 SIGTERM·SIGINT를 모두 기본 처리로 되돌린 뒤 다시 보냅니다. 비대화형 shell의 background 실행처럼 SIGINT를 무시한 채 시작하면, v6.0.1은 SIGINT에 거짓 `reason=signal` 줄을 남긴 채 살아 있고 이후 SIGTERM을 삼켜 SIGKILL(137)로만 끝났습니다. 이제 SIGINT는 계속 무시되고, SIGTERM은 원인 줄 하나를 남기고 143으로 종료합니다. systemd·Docker 기동은 signal을 기본 처리로 두므로 운영 동작은 같습니다.
- native 실패 복원(`restore_native_after_failed_cutover`)이 `if ! ( set -e; … )` 안에서 set -e가 무시되어, 복원 단계가 실패해도 계속 진행하고 `could not be restored` 경고를 내지 않던 기존 결함을 고칩니다. 이제 첫 실패에서 멈추고 경고를 남기며, 종료 상태는 그대로 원래 cutover 실패 상태입니다. 배포 테스트는 cutover 최상위 정지 단계, 실패 복원 경로, 수동 rollback이 원격에 보내는 payload 각각에서 PO socket을 collector `disable --now`보다 먼저 멈추는지 가짜 systemctl로 검사합니다. 복원 단계 실패도 검사하며, 호출부를 바꾸는 변이는 모두 이 검사에서 실패합니다.
- 문서를 바로잡습니다. `worker_failed`는 요청이 직렬 슬롯을 쥔 동안의 종료이고, `worker_exited`는 직렬 슬롯이 비어 있을 때의 종료입니다. v6.0.1의 "worker IO 중 종료"보다 범위가 넓습니다. 종료 줄은 SIGTERM·SIGINT, 퇴역, listener·기동 실패에만 남고 SIGHUP·SIGQUIT·panic·SIGKILL에는 남지 않습니다. 커널 경쟁의 남은 창도 적었습니다. worker 사망 경로는 µs, `session_closed`·`lease_expired`·`worker_timeout`은 수 ms reap 간격이고, bootstrap 실패 뒤 `retireOwned` DELETE가 반복되는 대표 트리거입니다. native 산출물 version은 `HOLO_BOT_VERSION=<ver>`로 넘겨야 합니다(없으면 short SHA). API image를 재빌드하지 않아도 이후 중앙 전체 `compose up`은 API `APP_VERSION`을 repo 릴리스로 바꾸고, image label은 이전 build로 남습니다. v6.0.1의 "collector 기동도 `enable` 뒤 `restart` 한 번으로 줄였습니다"는 사실과 다릅니다. 이전 `enable --now`도 멈춘 collector를 한 번 기동했으므로, 이 변경은 기동을 `enable` + `restart`로 명시한 것일 뿐 횟수는 같습니다.
- PO를 먼저 멈춘 cutover 창에서 이전 collector의 mint·반납 실패는 helper `/health`의 `proof.last_error`에만 남고 로그 줄을 만들지 않습니다. 그래서 cutover journal 오류 검사가 거짓 실패하지 않으며, 검사 범위는 바꾸지 않았습니다.
- collector·PO 산출물 버전은 `6.0.2`입니다(루트 `VERSION`, `hololive/hololive-api/VERSION`). API는 코드 변경이 없어 재배포하지 않고, alarm-worker는 `5.0.0`을 유지합니다. retry·fallback은 추가하지 않았고 DB migration·운영 설정·공개 API는 바꾸지 않습니다.

## v6.0.1 - 2026-09-28

- `po-broker`는 종료할 때 stderr에 `po-broker exit reason=<reason> generation=<uuid>` 한 줄만 남깁니다. `reason`은 실제 퇴역 호출 지점에서 정한 고정 어휘입니다. `session_closed`(DELETE /v1/session), `lease_expired`(lease 타이머·요청 시점 만료), `worker_failed`(요청의 worker IO 중 종료 포함), `worker_timeout`, `request_aborted`(worker IO 중 요청 연결 끊김), `response_failed`, `worker_exited`(worker IO 요청이 없는 동안 기동을 마친 worker의 자체 종료), `startup_failed`, `listener_failed`, `signal`(퇴역 전 SIGTERM·SIGINT)이 있습니다. 여러 경로가 동시에 퇴역을 요청해도 처음 표시된 원인만 남고, 퇴역 중 받은 signal은 먼저 표시된 원인을 남깁니다. token·payload·요청 본문·worker 출력은 기록하지 않습니다. exit code는 그대로이고, signal 종료는 원인을 기록한 뒤 같은 signal을 기본 처리로 다시 보내 종료 상태(예: 143)를 유지합니다. 그동안 로그 없이 exit 0으로 끝나던 issuer 재시작도 이제 원인별로 구분됩니다.
- AppArmor af_unix 미디에이션 경쟁(upstream `b1aea2c19607`, 현재 Ubuntu linux-oracle 빌드 미포함)으로 생긴 커널 Oops(`unix_fs_perm+0xd0`)를 완화합니다. broker는 응답마다 keep-alive를 끄고 연결을 닫았고, 이 close가 youtubejs helper의 새 연결 첫 read와 겹치는 창에서 Oops가 났습니다. 이제 broker HTTP keep-alive를 켜서 정상 응답 뒤에는 서버가 연결을 닫지 않습니다. helper의 `ProofBrokerClient` Agent는 `timeout: 1000`으로 유휴 socket을 broker `IdleTimeout`(2초)보다 먼저 닫습니다. 그래서 유휴 연결은 client가 끝내고, 서버가 닫는 중인 socket에 요청을 보내는 재사용 경쟁도 생기지 않습니다. 퇴역 시에는 `server.Close`가 유휴·진행 중 연결을 즉시 닫으므로 프로세스도 바로 종료합니다. generation·lease·operationLimit·header/body 한도 검사는 요청마다 그대로 적용합니다. retry·fallback은 추가하지 않았습니다. 근본 수정은 커널 교체이며, 퇴역·오류 응답 직후의 새 연결 첫 read 창은 남아 있습니다.
- native AP(`collector-a`·`collector-d`) cutover·실패 복원·수동 rollback은 PO socket·service를 collector보다 먼저 멈춥니다. collector의 종료 generation 반납이 살아 있는 broker를 퇴역시켜 `Restart=always`가 곧 멈출 issuer를 다시 띄우던 재시작을 없앴고, collector 기동도 `enable` 뒤 `restart` 한 번으로 줄였습니다. 운영 runbook은 issuer의 Docker `RestartCount`·systemd `NRestarts`를 generation 교체 횟수로 정의하고 종료 코드·OOM·health·generation 일치로 합격을 판정합니다. Compose issuer-first cutover의 1회 issuer 재시작은 예상 동작으로 문서화했습니다.
- collector·PO 산출물 버전은 `6.0.1`입니다. collector·PO image는 `hololive/hololive-api/VERSION`을 씁니다. 같은 파일을 쓰는 API는 코드 변경이 없어 재배포하지 않고 `6.0.0` 산출물을 유지하며, alarm-worker도 `5.0.0`을 유지합니다. DB migration·운영 설정·공개 API는 바꾸지 않습니다.

## v6.0.0 - 2026-09-28

- `iris-client-go/v3 v3.0.2`으로 API·alarm-worker·shared·collector·DB 테스트 모듈을 함께 이관합니다. 웹훅 본문·방은 `Message.Msg`·`Message.Room`에서 읽고, 서명된 요청은 body `messageId`를 헤더와 일치시킵니다. API·alarm-worker 산출물 버전은 각각 `6.0.0`·`5.0.0`입니다.
- migration 228은 검토한 격리 send unit의 `closed_without_replay` 감사 영수증을 추가합니다. 정확한 전체 대상 ID·revision·상태 메타데이터와 원본 전체 SHA-256 digest를 저장하고, payload·본문·오류 원문과 원본 delivery 상태는 복사하거나 바꾸지 않습니다. maintenance 작업은 직렬화 트랜잭션에서 잠금·digest를 재검사하고 send unit당 한 건만 기록하며, 결과 불명은 receipt 조회로 판정합니다. 정상 retention은 그대로 적용됩니다.
- migration 229는 227 적용 기록과 현재 singleton의 완료·버전·cursor를 잠금 아래 다시 확인한 뒤 일회성 `youtube_notification_delivery_ledger_state`를 제거하고 229 적용 기록을 같은 transaction에 씁니다. 빈 DB는 원본 outbox·delivery·logical ledger가 모두 비어 있을 때만 허용하고, 기록 없는 table 부재는 거절합니다. 적용 전 운영 singleton을 복구용으로 보존하며 logical delivery ledger는 유지합니다.
- migration 230은 현재·보존·지원 rollback API 이미지가 terminal payload를 직접 비우는 writer를 포함함을 확인한 뒤, `bot_webhook_inbox`의 구 호환 scrub trigger와 함수만 원자적으로 제거합니다. 적용 전에 검증된 terminal payload CHECK와 정확한 trigger/function catalog 형태를 검사하고, CHECK는 남깁니다. 구 status-only writer는 CHECK에 거절되며 현행 complete·abandon·release·reclaim은 terminal scrub과 retry payload 보존을 계속 담당합니다. 의존성 오류는 전체 DROP을 롤백합니다. 수동 DDL 재설치는 230 적용 ledger와 어긋나므로 rollback이 아닙니다.
- 사용하지 않는 alarm-worker profile의 `notification_delivery.lock_timeout_ms`를 exact-key 설정과 fixture에서 제거합니다. 운영 profile에서도 같은 rollout 전에 키를 지워야 새 decoder가 기동하며, 실제 delivery lease·retry 동작은 기존 정본을 따릅니다.

- native PO 배포 fragment가 앞서 적재한 helper의 service/socket/unit 입력을 명시적으로 요구합니다. helper 없이 실행하면 payload·호스트 변경 전에 실패하며 값의 단일 소유자는 그대로입니다. readiness 관측 loop는 미사용 변수를 `_`로 명시하고 기존 30회·2초 상한을 유지합니다. meta ShellCheck 경고를 억제하지 않고 해소했으며 application binary·운영 설정·프로토콜은 바꾸지 않습니다.

## v5.0.1 - 2026-09-28

- DB pool이 `POSTGRES_SSLROOTCERT`를 pgxdb `Config.SSLRootCert`로 직접 넘기는 v5.0.0 배포 뒤의 lockstep 릴리스입니다. API·alarm-worker·shared·collector·DB 테스트 모듈과 정기 보안 검사의 sibling checkout을 `shared-go/v2 v2.8.0`(`36d47654f2c0`)과 `iris-client-go/v2 v2.8.0`(`b23933c3179c`)으로 함께 고정합니다. shared-go v2.8.0이 지운 pgxdb env 폴백과 호환 API는 v5.0.0에서 이미 대체 경로로 옮겨 이 고정에 필요한 코드 변경은 없고, 다른 의존성 버전도 바꾸지 않습니다.
- 운영 산출물은 API·collector `5.0.1`, alarm worker `4.0.1`로 식별합니다. DB migration·런타임 설정·공개 API는 변경하지 않습니다.
- T11의 Community/Shorts `post_id` 단일화를 마무리합니다. claim과 telemetry가 같은 canonical logical ID 검증을 사용하고, 누락·파싱 실패·불일치를 `content_id`·payload 리소스 ID로 대체하지 않습니다. 잘못된 claim ID는 오류로, 로그 ID는 원문을 포함하지 않는 `invalid:<kind>:<reason>`으로 드러냅니다. telemetry batch의 빈 post ID는 INSERT 전에 전체 batch를 거절하며, NEW_VIDEO·LIVE·MILESTONE의 content ID 계약은 유지합니다. 운영 canonical ID 존재 확인과 실제 PostgreSQL의 수정 전 실패·수정 후 거절 회귀, canonical consumer smoke를 거쳤습니다. 새 retry·fallback은 추가하지 않습니다.
- telemetry의 빈 `delivery_path`도 enrichment·INSERT 전에 전체 batch를 거절합니다. 빈 값을 `youtube_outbox_dispatcher`로 보충하던 경로를 없애고, 전이 기록자는 정본 상수를 직접 쓰며 방출 로그는 저장된 경로를 trim할 뿐 대체하지 않습니다.

## v5.0.0 - 2026-09-28

- API·alarm-worker·shared·collector·DB 테스트 모듈의 `iris-client-go/v2`를 게시된 `v2.7.0`으로 고정합니다. 정본 Karing wire 이름과 결과 불명 오류 보존, 서버 퇴역 사전 이관 계약을 같은 SDK 버전으로 검증합니다.
- `shared-go/v2`를 검증된 `v2.7.2`로 고정해 inbox prune의 호출당 삭제 상한을 보장합니다. 이 패치에는 S19 공개 API·TLS env 폴백 제거가 없으며, 소비자 TLS 명시 배포 뒤의 lockstep 릴리스와 구분합니다.
- local CI의 standalone `staticcheck`를 `scripts/ci/staticcheck-facts/build.sh` 고정 profile로 바꿉니다. 원인: `staticcheck 2026.2.1`(`honnef.co/go/tools v0.8.1`)이 쓰는 x/tools `v0.44.1-0.20260420230617-19499e7caabc`의 objectpath는 source에서 선언 순서대로 보이는 generic method를 export data에서는 일반 method 뒤로 읽어, `iris-client-go/v2` `APIClient`의 `SendKaringHololive` 폐기 fact를 `SendReaction`에 붙이는 거짓 SA1019를 냈습니다(cold cache에서도 재현). profile은 두 module의 sumdb hash와 zip SHA-256을 검증한 archive에서 x/tools revision을 바꾸지 않고 method 순서 정규화 패치만 적용해 go1.27.1로 빌드하고, 정상 호출은 경고 없이·실제 폐기 호출은 SA1019 1건으로 끝나는 source/export 경계 CLI fixture를 통과한 binary만 cache합니다. 경고 억제나 app·SDK 우회는 없습니다. 제거 조건: upstream x/tools objectpath 수정이 staticcheck release에 반영되고 같은 fixture가 무패치 binary로 통과하면 profile을 지우고 pinned install로 돌아갑니다.
- 알림 metric 수집기를 `sync.OnceValue`가 완성된 묶음으로 반환하게 합니다. 등록 중 panic 후 복구되더라도 다음 호출이 미초기화 전역을 읽지 않고 원래 등록 오류를 유지합니다. 지표 이름·label·등록 시점은 그대로입니다.
- 최종 알림 payload 회귀 fixture도 실제 claim의 저장된 send-unit ID와 기동 시 message_strings 적재·검증을 사용합니다. 전체 문구를 고정하던 알람 목록 테스트는 제거하고 접힘 경계·본문 보존 검증은 유지합니다.
- PostgreSQL 18 전용 DB fixture는 `DROP DATABASE ... WITH (FORCE)` 오류를 그대로 반환합니다. 최초 오류를 버리고 만료된 context로 재시도하던 PG<13용 terminate/plain-drop 경로를 제거했으며, 30초 정리 기한과 병렬도는 유지합니다. integration 전용 6개 패키지에서 실제 생성·삭제를 검증했습니다.
- PO·W4 통합 뒤 Compose 첫 배포 실패가 중지된 candidate image·issuer receipt를 남겨 다음 전환을 막던 상태를 고칩니다. 소유 container·tag·active receipt만 제거하고 source snapshot을 복원하며 volume·복구 archive는 보존합니다. collector 기준점 없는 기존 issuer는 전환 전에 거절하고, rollback 기준점 조회도 source 교체 전에 끝냅니다. 실제 Docker 정지·정리·source 복원 smoke와 실패-before/passing-after 상태 회귀로 검증했습니다.
- 중앙 collector·issuer paired cutover는 Compose의 collector·migrator DB 경로가 같음을 확인하고 migrator의 접속·TLS·network로 migration 222의 적용 checksum을 읽습니다. 외부 DB override를 로컬 socket ledger로 대신 검증하지 않습니다. 실제 두 PostgreSQL 인스턴스에서 대상 DB만 정상인 성공 경계와 checksum·경로 불일치 거절을 확인했습니다. 조회용 container·volume은 검증 후 제거하며 서비스와 DB는 변경하지 않습니다. 실행 계획은 완료된 최초 PO 계획 대신 현재 승인된 활성 `PO_PLAN_ID`를 필수로 받아 같은 strict gate를 통과시킵니다.
- native 완료 검사 fixture는 issuer 도입 전 rollback 상태를 명시하고 예상 issuer가 빠진 release를 거절합니다. polling 횟수·운영 지연은 유지하고 fixture의 대기만 기록하여 실패 사례가 실제 시간 경과에 의존하지 않게 합니다. 소스 문자열·로그 문구 고정 검사는 상태·결과 검증으로 대체합니다.
- retention 관측 timeout 회귀는 관측이 폐기할 연결과 삭제가 사용할 연결을 미리 엽니다. 새 연결 준비 지연을 250ms SQL 예산에 섞지 않으며 실제 timeout·SQL 오류 뒤 삭제의 독립 context와 삭제 횟수 검증은 유지합니다. production의 timeout·pool 설정은 바꾸지 않습니다.
- alarm-worker 알림은 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`, `DEC-20260904-hololive-karing-regular-chat-egress` 대체, PLN-20260926-stack-audit-refactoring T19). Karing 선택 분기·chunk planner·Karing sender·`karing` message_strings 요구·`karing.kakaolink` 계약 문서와 CI의 Karing 필수 문자열 검사를 삭제하고, CI는 alarm-worker에 Karing SDK 호출이 다시 들어오면 실패합니다. Markdown handoff 분류는 `ErrReplyHandoffOutcomeUnknown`·`ErrReplyHandoffFailed`로 이름을 바꿨습니다. 방송 알림 그룹의 ambiguous 발송 실패는 방 기준 quarantine 대신 다른 source와 같이 저장된 send-unit ID로 재시도합니다.
- 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꿉니다(`DEC-20260926-hololive-source-fallbacks-retirement`, T19). Holodex `GetChannel`의 scraper 부분 Channel, `GetChannels`의 개별 조회 보충, `GetChannelSchedule`의 YouTube·공식 일정 보충과 5분 재캐시, `GetRecentVideos`의 RSS 폴백·빈 성공·RSS backoff, matcher의 Holodex 보강과 채널명 폴백 체인, major event 요약 실패의 이벤트 목록 대체, `/api/holo` rate limit fail-open을 삭제했습니다. `domain.MemberDataProvider`는 오류를 돌려주는 `LoadAllMembers` 하나로 바꿨습니다. 알림 멤버 표시명 폴백만 예외 계약(trigger·한도·telemetry·owner·제거 조건)으로 남고 `hololive_alarm_member_name_fallback_channels`·`hololive_alarm_member_name_caller_fallback_total`로 관측합니다. `YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS`와 channel schedule 보충 캐시 TTL이던 `OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS`는 빈 값이어도 존재 기준 퇴역 가드가 hololive-api·alarm-worker 기동을 거절하고(remove_after 2026-12-31), `hololive_admin_rate_limit_fail_open_total`은 `hololive_admin_rate_limit_check_failures_total`로 바뀝니다. major event 요약이 실패한 주·월의 알림은 자동으로 다시 보내지 않으므로 `/internal/trigger/majorevent-*` 수동 trigger로 복구합니다.
- 감사 계약 없는 fallback을 제거합니다(PLN-20260926-stack-audit-refactoring T09). live-status YouTube scraper 2차 경로와 `HOLODEX_LIVE_STATUS_FALLBACK_*` 설정을 삭제하고, 남은 키는 존재 기준 퇴역 가드가 기동을 거절합니다(`DEC-20260926-hololive-live-status-scraper-fallback-removal`).
- LLM 월 토큰 상한과 Valkey 월 카운터를 삭제하고 `hololive_llm_cost_tokens_total`을 항상 기록합니다. `LLM_MONTHLY_TOKEN_CEILING`은 퇴역 가드가 거절합니다(`DEC-20260926-hololive-llm-token-ceiling-retirement`).
- message_strings를 bot·llm plane과 alarm-worker 기동 때 한 번 적재해 필수 key를 검증하고, 실패하면 기동에 실패합니다. 코드 대체 문구·lazy 재적재·`FallbackSentinel`을 제거하고 시드 SQL 기반 format 인자 수 테스트를 둡니다. 템플릿 렌더 실패 응답은 DB의 `command_processing_failed` 문구이며, 예약 알림 렌더 실패는 발송하지 않고 오류로 남깁니다(`DEC-20260926-hololive-message-strings-startup-validation`).
- `org=all` 부분 결과는 `PartialStreamsError`로 알리고 캐시하지 않으며, 공식 일정 fallback의 빈 결과를 빈 성공으로 캐시하지 않습니다. 이에 따라 Stream HTTP API `/api/holo/streams/{live,upcoming}?org=all`은 일부 org만 실패해도 부분 목록 대신 500을 응답합니다(이전: 부분 목록 200). channel schedule 보충 경로는 T19에서 provider의 5분 재캐시와 함께 삭제했습니다(아래 원천 폴백 항목).
- 숫자·bool·기간 env의 잘못된 값을 경고 뒤 기본값으로 바꾸지 않고 기동 실패로 드러냅니다(PLN-20260926-stack-audit-refactoring T10, 감사 B4). hololive-api·alarm-worker·youtube-collector 설정 로더와 alarm-worker의 `CELEBRATION_RUNNER_ENABLED`·`BIRTHDAY_STREAM_RUNNER_ENABLED`가 shared-go 엄격 파서(`IntE`·`Int64E`·`BoolE`·`FloatE`)를 쓰고, 이관한 초·밀리초 단위 기간 키(`*_SECONDS`·`*_MS`)가 `time.Duration` 범위를 넘으면 거절합니다. 한 로더 구획(bot·admin 공통 구획, llm plane, collector 공통 구획 등) 안의 잘못된 키는 한 번의 실패에 함께 보고되지만, 구획 사이에서는 처음 실패한 구획에서 멈춥니다. 여러 구획이 잘못됐으면 앞 구획을 고친 뒤 다시 기동해야 다음 구획의 오류가 보입니다. 값이 없거나 비어 있으면 지금처럼 기본값입니다.
- env 이름 alias 체인을 지웁니다. `HOLODEX_API_KEY`와 `SERVICES_LLM_SCHEDULER_HEALTH_URL` 하나씩만 읽고, 퇴역한 `HOLODEX_API_KEY_1`·`SERVICES_LLM_SERVER_HEALTH_URL`은 빈 값이어도 존재만으로 기동을 거절합니다(`DEC-20260926-hololive-legacy-env-config-retirement`). prod compose는 `HOLODEX_API_KEY_1`을 더 주입하지 않고, host-native collector env 생성기는 소비자가 없는 `HOLOLIVE_H3_SERVER_NAME`을 쓰지 않습니다. 배포 전에 모든 hololive env에서 두 퇴역 키를 지워야 합니다.
- dedup 선점·ACL 캐시 동기화·member news 요약·내부 H3 client·DB 역할 비밀번호의 fallback을 오류 반환 또는 기동 실패로 바꿉니다. 활성 member news LLM client 초기화 실패, 설정한 X allowlist 로드 실패, 내부 H3 전용 env 누락, 역할 비밀번호 누락은 모두 실패로 드러납니다. 내부 JSON client(`internalhttp.NewJSONClient`)는 H3 구성 실패 때 경고 뒤 TCP client로 내려가지 않고 오류를 돌려주며, 설정된 LLM scheduler URL이나 bot 내부 URL로 client를 만들지 못하면 bot·admin plane 기동이 실패합니다.
- 퇴역 가드와 드레인 종단에 제거 조건을 둡니다(PLN-20260926-stack-audit-refactoring T17). `MEMBER_NEWS_CLIPROXY_MODEL`·`DB_SSLMODE`·`DB_QUERY_EXEC_MODE`·`OTEL_ENVIRONMENT`는 빈 값이어도 존재만으로 hololive-api·alarm-worker·youtube-collector 기동을 거절하고(이전: 비어 있지 않을 때만), `HOLOLIVE_X_SPACES_LOGIN_ENABLED`는 compose env 파일뿐 아니라 호출 프로세스 env에 있어도 `compose.sh`가 멈춥니다. 퇴역 가드마다 도입 리비전·제거 조건·`remove_after` 재검토 기한을 적었고, 표준 `OTEL_EXPORTER_OTLP_ENDPOINT`·`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 거부와 재도입 방지 CI 게이트는 제거 조건 없는 영구 계약으로 이름과 문서를 고쳤습니다.
- 중앙 배포(`build-all.sh`·`compose.sh`·`compose-redeploy-service.sh`)는 퇴역 runtime 컨테이너 12종을 stop/rm 하거나 부재를 단언하지 않습니다. T18(2026-09-26)에서 중앙·AP에 퇴역 runtime 컨테이너·이미지가 0개였고 그 상태로 release가 배포돼 `removed-runtimes.sh`와 호출·테스트·sync 목록을 지웠습니다. 재도입은 `test-three-runtime-topology.sh` 정적 gate가 막고, compose up 순서 검사는 `test-compose-up-flow.sh`로 옮겼습니다(`DEC-20260926-hololive-retired-rollback-tooling`, T17·T19).
- alarm dispatch claim이 send unit 없는 migration 141 이전 delivery를 읽던 `legacy_head`를 지우고, Text 경로는 저장된 send-unit `client_request_id`가 없으면 파생 ID로 보내지 않고 발송 전 실패로 드러냅니다. 관리 재처리는 send unit 없는 행을 `group_identity_mismatch`로 막고, migration 224의 `alarm_dispatch_deliveries_active_send_unit_check`가 send unit 없는 delivery의 pending·retry·leased·sending 기록을 거절합니다(수동 requeue 스크립트 포함). 224의 VALIDATE는 활성 NULL 행이 있으면 실패합니다. Twitch/Chzzk 단독 보관 envelope는 error 로그를 남기고 DLQ로 가며 렌더러의 같은 분기는 지웠습니다.
- migration runner는 manifest 밖 `schema_migrations` 행을 182 적용 여부와 관계없이 즉시 거부합니다. epoch-1 ledger 잔재와 checkpoint baseline 수용 창은 닫혔습니다.
- migration 222가 177/189의 `legacy_collector` 진단 backfill trigger와 함수를 지우고, collector release는 복원 단계 없이 `last_failure_*`를 건드리지 않습니다. 222는 새 collector보다 먼저 중앙 `db-migrate`로 적용해야 합니다. migration 223은 `bot_webhook_inbox` 호환 scrub trigger가 payload를 비우지 않는 writer를 만나면 PostgreSQL WARNING을 남기게 합니다. 222·223과 위의 224는 운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음 번호입니다.
- 구형 코드와 끝난 전환 경로를 지웁니다(PLN-20260926-stack-audit-refactoring T11). celebration은 전환일 legacy identity 읽기를 지우고 `MemberID`만으로 식별하며, `MemberID`가 없는 celebration 발행은 canonical 검증에서 거절됩니다(dispatch group·identity의 ChannelID 대체 삭제). alarm dispatch payload의 `notification.alarm_type` 누락은 Live로 채우지 않고 decode 오류이고, NEW_SHORT raw content_id 재사용과 tracking raw 후보 조회, member point 캐시의 채널·이름 식별 폴백도 지웠습니다(T18 잔존 0건).
- YouTube scraper에서 production 호출자가 없던 browser snapshot fetch·진단, proxy 연속 실패 시 direct 자동 전환 정책, `SCRAPER_FETCHER_ENGINE` 선택, producer active-active 설정 잔재를 지웁니다. `hololive_youtube_scraper_fetch_fallback_total` metric은 생산자가 없어 삭제했고 fetch metric의 `engine` 라벨은 `nethttp`로 유지합니다. `SCRAPER_FETCHER_ENGINE`·`SCRAPER_BROWSER_DIAGNOSTIC_*`·`YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT`·`IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS`는 빈 값이어도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31). production은 `IRIS_BASE_URL_FILE` stat 검사를 항상 하고 live-compat overlay는 우회 플래그를 더 주입하지 않습니다. SOCKS5 proxy와 관리 설정 `scraperProxyEnabled`는 iris-console 소비자와 함께 T19에서 다룹니다.
- 관리 화면이 저장한 `settings.json`이 targetMinutes 없는 구형 형식이거나 읽을 수 없으면 alarm-worker가 설정값으로 대신하지 않고 기동에 실패합니다(파일이 없으면 설정값 사용). v2 delivery locker는 cache가 없을 때 dedup을 끈 noop locker로 내려가지 않고 생성 오류이며, digest scheduler는 locker 없이 실행하지 않습니다. bot 응답 formatter의 퇴역 Chzzk/Twitch 분기와 멤버 프로필의 치지직 링크를 지웠습니다(저장된 식별자는 보존).
- 운영 도구를 정리합니다. epoch-2에서 거부만 하던 `apply-all.sh`·`bootstrap-and-apply.sh`, send-unit 이전 rollback preflight, 퇴역 Redis dispatch queue용 `requeue-alarm-dlq.sh`와 그 CI 리터럴 고정을 삭제했습니다. AP 배포·rollback은 퇴역 producer 첫 cutover 상태 기록·복원과 repo 루트 compose 경로 폴백 없이 동작하며, 기록된 `rollback-image-tag`(Compose)나 `previous` release(host-native)가 없으면 rollback을 거절합니다. `rollback-image-tag`가 있는 Compose AP 배포가 cutover 뒤 실패하면 이전 collector와 issuer를 함께 자동 복원하고, 없는 첫 배포는 새 collector와 issuer 컨테이너를 멈추고 fix-forward를 안내합니다. 중앙 compose up/down은 live-compat overlay를 항상 넣고 `HOLOLIVE_ENABLE_LIVE_COMPAT` drop-in과 빈 main-ap overlay 2종·profile을 지웠습니다. AlarmService의 미사용 Holodex 의존성을 지우고, `youtube:producer` Valkey 키 접두사는 전환 계획 없이 바꾸지 않은 채 파일명만 정리했습니다.
- `IRIS_WEBHOOK_REQUIRE_HMAC` 주입(prod compose `x-iris-env`, `.env.example`)과 `Webhook.RequireHMAC` 필드·값 가드를 지우고, 키가 남아 있으면 빈 값이나 `true`여도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31, 계획 T11 C8). 배포 전에 중앙 compose.env와 bot.env·alarm-worker.env, 그 stack-secrets master에서 키를 지워야 합니다.
- `live_snapshot` contract generation 1 decoder·supported contract 항목과 collector의 generation 1 payload 경로를 지웁니다(C6 runbook 조건은 T18에서 충족). API는 generation 1 관측을 unsupported contract로 처리하고, collector는 DB current generation이 2가 아니면 `configuration_error`로 수집을 끝냅니다. migration 225는 빈 DB의 `live_snapshot` seed를 generation 2로 맞추며 운영 DB에서는 갱신 대상이 없습니다(운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음 번호).
- hololive-api settings 서비스도 `settings.json` 해석을 `settings.ReadFile` 하나로 합쳐, 읽기·decode 실패나 구형 형식을 기본값으로 대신하지 않고 bot·admin plane 기동 실패로 드러내며, 정규화 결과를 파일에 다시 쓰지 않습니다.
- dispatch 발행은 `Version` 0을 V1으로 채우지 않고 거절하며(celebration·birthday stream·X Space 생산자가 명시), dispatch consumer는 claim key releaser가 필수라 없으면 해제 오류를 돌려줍니다. member cache는 분산 캐시가 있으면 epoch authority가 필수이고 epoch 없는 구형 `member:*` keyspace 분기를 지웠습니다. collector는 구 helper failure class를 기본 class로 고치지 않고, 계약 밖 tuple을 미분류 `collection_internal_invariant`로 닫아 지연 처리하며 `youtube_collection_invalid_failure_tuple_total`로 셉니다.
- 잔존 0건이 확인된 legacy 수용을 지웁니다. v2 delivery outbox의 lease 이전(`locked_by` NULL) lock 회수·fence와 `FetchAndLock`의 lockTimeout 인자(worker profile `notification_delivery.lock_timeout_ms`는 이제 읽지도 양수 검증하지도 않지만, exact-key profile 계약 때문에 stack-secrets profile에서 함께 지울 때까지 키는 남음), 빈 PARTIAL Shorts watermark 무시 규칙, 비정규 YouTube source identity의 해시 수용(이제 dispatch 원장 저장 전에 거절), YouTube dispatch payload의 `render_template_key`·`pre_rendered_message`, 퇴역 Chzzk/Twitch·Redis dispatch queue Valkey 키의 예약 항목과 상수(배포 전 잔존 키 3개를 1회 삭제)입니다.
- 호출자 없는 코드와 끝난 도구를 지웁니다. AlarmQueueEnvelope 미사용 payload accessor, 테스트 전용 LLM scheduler 비-runtime 로더, 폐기된 `ChannelTargetGroupStats` alias, settlement archive migration·수동 drop 스크립트·퇴역 runbook, Osaka legacy log rollup 설치 스크립트, ap-status의 AP에 없는 신호·split-host 볼륨 진단, migration manifest 검사의 epoch-1 예외 목록, 끝난 전환을 소스 텍스트로 고정하던 테스트입니다. `alarm.dispatch` 계약 문서(CONTRACT_MANIFEST·CONTRACT_MAP·contracts/alarm·QUEUE_AND_PUBSUB_CONTRACTS·dlq-replay runbook·README)는 퇴역 Redis queue와 version 0 수용 대신 PostgreSQL dispatch outbox 기준으로 고쳤습니다.
- alarm-worker의 `ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH`·`CELEBRATION_CHECK_HOUR_KST`·`CELEBRATION_RUN_INTERVAL_MS`·`BIRTHDAY_STREAM_POLL_INTERVAL_MS`·`BIRTHDAY_STREAM_SESSION_FRESHNESS_MS`가 정수가 아니거나 범위를 벗어나면 경고 없이 기본값으로 바꾸지 않고 기동에 실패합니다(PLN-20260926-stack-audit-refactoring T10 후속). `CELEBRATION_CHECK_HOUR_KST`는 0–23만 받고(이전: 24 이상도 받아 날짜 정규화로 설정과 다른 시각에 실행됨), celebration·birthday stream 키는 해당 runner가 켜졌을 때 infra 확인보다 먼저 읽습니다. 값이 없거나 비어 있으면 지금처럼 기본값입니다. 배포 전에 alarm-worker env에 이 키가 있으면 값이 파싱되는지 확인해야 합니다.
- 품질 항목을 정리합니다(PLN-20260926-stack-audit-refactoring T12). nil context를 `context.Background()`로 바꾸거나 건너뛰거나 따로 오류로 돌려주던 분기(hololive-api·alarm-worker·youtube-collector·hololive-shared 30여 곳)를 지웠습니다. context는 호출자가 항상 넘기는 계약이며 nil을 넘기면 호출 지점의 결함입니다. 생성형 결과 이름(`value0`·`ok1`·`result1` 등)을 정리했고, production 코드의 단일 인자 `errors.Join(x)` 감싸기(변수 인자와 같은 패키지 helper 호출 인자 모두)를 직접 반환으로 바꿨습니다. 오류 문자열과 `errors.Is`·`errors.As` 판정은 그대로입니다. 캘린더 사진 렌더러의 테스트 전용 래퍼 `fetchMemberPhoto`·`fetchImage`를 지우고 테스트가 context를 받는 함수를 직접 부릅니다. youtubedispatch 테스트의 `deliveryTestDB` 호환 alias와, T18에서 운영 collector env 0건을 확인한 collector 퇴역 alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`·`YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`) 무시 고정 테스트를 지웠습니다(prod compose의 collector canonical 기본값 렌더 검사는 별도 테스트로 유지). Holodex cache-fill의 원천 장애 꼬리 지연은 측정 기록만 남겼습니다(`docs/current/services/hololive-api.md`).
- outbox v3 handoff를 삭제하고 v1 YouTube 알림과 v2 digest의 direct egress를 정본으로 둡니다(`DEC-20260926-hololive-outbox-v3-convergence`, PLN-20260926-stack-audit-refactoring T19). `off`·`shadow`·`cutover` 분기, `PublishShadowDispatchBatch`, `alarm/handoff` 패키지, handoff metric(`hololive_youtube_outbox_v3_handoff_total`·`hololive_delivery_outbox_v3_handoff_total`), shadowed retention을 지웠습니다. `YOUTUBE_OUTBOX_V3_HANDOFF_MODE`·`DELIVERY_OUTBOX_V3_HANDOFF_MODE`는 prod compose에서 주입하지 않고, 빈 값이어도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31). migration 226이 `alarm_dispatch_deliveries` 상태 CHECK에서 `shadowed`를 빼고, 적재 SQL은 shadowed 승격 UPDATE 없이 `ON CONFLICT (dedupe_key) DO NOTHING`입니다. 관리 조회 필터 `status=shadowed`는 iris-console enum과 필터 제거 배포 뒤 삭제했으며, 다른 알 수 없는 상태처럼 400으로 거절합니다. 226은 225 다음 번호입니다.
- YouTube delivery telemetry를 lifecycle 전이 트랜잭션 안의 단일 기록으로 바꿉니다(`DEC-20260926-hololive-delivery-telemetry-single-path`). `TransitionStore`가 `CompleteSent`·실패 전이·stale SENDING 격리 트랜잭션에서 owner 시도 하나당 한 행을 쓰고, 호출자는 `per_room`·`grouped` 발송 방식만 넘깁니다. prepared failure(`message_missing`·`pre_send_claim` 포함)도 기록하고, 결과 불명 격리는 `outcome_unknown`·`stale_sweep`으로 남깁니다. `failure_reason`은 lifecycle Reason 코드이고 `post_id`는 검증된 logical key입니다. `attempt_ordinal`은 버퍼에 남은 delivery별 최대 순번 다음 값과 claim 시점 `attempt_count + 1` 중 큰 값이라 revive 뒤나 telemetry retention(기본 24h) 정리 뒤에도 되돌아가지 않습니다. 버퍼 행이 지워진 뒤 revive된 delivery만 1부터 다시 세고, 같은 시도 식별자는 조용히 건너뛰지 않고 오류로 전이를 rollback합니다. commit 뒤 metrics recorder의 직접 enqueue와 `direct_fallback` 로그, 분류 재저장, delivery 테이블 역산 backfill(`recovered`)과 profile 키 `youtube_delivery.telemetry_backfill_batch`를 삭제했습니다. exact-key profile 계약 때문에 운영 alarm-worker profile의 키 제거는 이 release 배포와 같은 유지보수 단계에서 해야 하고, 이 release 이전 alarm-worker image로 rollback할 때는 키를 다시 넣어야 합니다(`docs/current/runbooks/rollback.md`). DEC 순서 조항에 따라 이 변경은 alarm-worker Karing dead branch 삭제(`DEC-20260926-hololive-karing-egress-disposition`)와 같은 release로 묶어 내보냅니다.
- v2 delivery locker가 Valkey `SetNX`·`CompareAndDelete`·`DelMany` 오류를 경고만 남기고 lock 없이 진행하지 않고 오류로 돌려줍니다. digest 실행은 lock 획득 오류로 실패해 다음 주기에 다시 시도됩니다.
- scraper proxy와 퇴역 producer 시대 설정을 지웁니다(`DEC-20260926-hololive-legacy-env-config-retirement`, `DEC-20260926-hololive-youtube-producer-budget-retired`, PLN-20260926-stack-audit-refactoring T19). 공개 계약 변경: admin 설정 API(`GET`·`POST /api/holo/settings`)는 `scraperProxyEnabled`를 받지 않고, 응답 runtime의 scraper proxy 항목과 `config_publish_scraper_proxy*`를 내지 않으며, 설정 파일에도 쓰지 않습니다(소비자 iris-console이 먼저 필드를 지움). `scraper_proxy` 설정 pub/sub type·runtime proxy 토글·SOCKS5 proxy client·scheduler proxy 전환, `ScraperConfig`의 scheduler·poll·snapshot·channel health·backfill 필드와 loader·검증·`*OrDefault`, 퇴역 producer의 scheduler·budget 코드(`ProvideScraperScheduler`, `scraper_scheduler_options.go`, `poller/runtime/scheduler`)를 삭제했습니다. `SCRAPER_PROXY_ENABLED`·`SCRAPER_PROXY_URL`·`SCRAPER_SCHEDULER_*`·`SCRAPER_POLL_*`·`SCRAPER_SNAPSHOT_*`·`SCRAPER_CHANNEL_HEALTH_*`·`SCRAPER_BACKFILL_*`와 가드 없이 무시되던 `SCRAPER_*_SECONDS`·`SCRAPER_WORKER_COUNT`·`IRIS_SHARED_TOKEN`은 빈 값이어도 hololive-api·alarm-worker 기동을 거절하고, youtube-collector는 `SCRAPER_PROXY_*`와 HC-013 이전 alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`·`YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`)를 같은 기준으로 거절합니다(remove_after 2026-12-31). 배포 전에 모든 youtube-collector env와 stack-secrets master에서 `SCRAPER_PROXY_*`를 지워야 합니다.
- youtube-collector YouTube.js helper의 proxy 경로와 `undici` 의존성을 지웁니다(`DEC-20260926-hololive-legacy-env-config-retirement` 후속, PLN-20260926-stack-audit-refactoring T16 인계). `SCRAPER_PROXY_*` 퇴역 뒤 collector는 늘 proxy 없이 bootstrap해 production에서 도달할 수 없던 Go `youtubejs.ProxyConfig`·`Config.Proxy`와 proxy URL 검증, bootstrap 요청의 `proxy`, bootstrap·`/health` 응답의 `proxy_enabled`, Node `ProxyAgent` transport·`loadUndici` 주입·proxy URL redaction과 이를 닫던 transport close timeout(`--shutdown-timeout-ms`)을 matched pair로 삭제했습니다. helper는 Node 내장 `fetch` 한 경로만 쓰며 재시도·취소·오류 분류는 그대로입니다. helper protocol 변경: bootstrap은 `protocol_version`과 `limits`만 받고 `proxy`는 unknown field로 거절합니다. collector와 helper는 같은 image로 함께 배포되며 섞인 조합은 bootstrap protocol mismatch로 기동에 실패합니다. collector hardening contract의 `new ProxyAgent` 필수 규칙(HC-002)도 지웠습니다.
- rate limiter와 dispatch worker 식별자는 hostname 하나만 씁니다. hostname을 얻지 못하면 random·`local`·`unknown-host`로 바꾸지 않고 limiter 생성과 alarm-worker 기동이 실패하며, 퇴역한 `INSTANCE_ID`는 분산 limiter 활성 여부와 무관하게 hololive-api·alarm-worker 설정 로드에서 빈 값이어도 거절합니다(T18: 운영 env에 없음). 표준 OTel endpoint env 거부는 퇴역 가드가 아닌 영구 계약으로 이름과 문서를 고쳤습니다. 코드와 맞지 않던 `DEC-20260512-hololive-alarm-http-provider-ownership`·`DEC-20260625-hololive-runtime-role-boundary`는 정정 레코드(`-v2`)가 대체합니다.
- 끝난 전환·rollback 도구를 지웁니다(`DEC-20260926-hololive-retired-rollback-tooling`, T19). rollback runbook의 5→3 긴급 rollback 절, epoch-1 복구 도구(`preflight-114-restore.sh`, `repair_message_contract_074_082.sh`, epoch-1 소스 사본과 source contract 검사)와 `db-maintenance-exec.sh`의 rollback 출력 마운트, epoch-2 baseline 생성기(`normalize-epoch2-baseline.py`는 `--check-existing` 검사만 남김), Dockerfile이 빌드하지 않던 `warm_member_cache`·`test_db_integration` 보조 cmd, admin-dashboard 퇴역 경로 가드와 SQL·DB 접근 검사의 admin-dashboard 대상을 삭제했습니다. YouTube delivery ledger backfill 명령·패키지·Dockerfile 빌드 줄과 alarm-worker `TransitionStore`의 전이마다 완료 표식을 읽던 gate도 지웠고, 완료 전제는 migration 227(`227_youtube_delivery_ledger_backfill_closed.sql`)이 적용 시점에 확인합니다. 227은 미완료 state가 있거나 backfill 없이 delivery·outbox 행이 있으면 실패하고, 운영 DB(T18: `completed_at` 있음)와 빈 DB는 통과합니다. 번호는 226 다음입니다.
- 방 ACL은 chatID 하나로만 판정합니다(`DEC-20260926-stack-hololive-room-acl-and-console-contract`, PLN-20260926-stack-audit-refactoring T19). `acl.Service.IsRoomAllowed`가 방 이름 인자를 받지 않아, 방 이름으로 등록된 값은 같은 이름의 다른 방을 허용하거나 차단하지 않습니다(T18: 운영 `acl_rooms` 3행 모두 숫자 chatID). 새 등록값도 chatID만 받습니다. 공개 계약 변경: admin `POST /api/holo/rooms`는 정확한 signed i64 chatID 문자열(부호·앞자리 0·범위 검사, iris-console `roomRegistration`과 같은 규칙)이 아니면 400으로 거절하며, 이미 저장된 비-chatID 값은 조회·제거만 할 수 있습니다. ACL 첫 초기화 seed `KAKAO_ROOMS`는 방 이름 기본값 `홀로라이브 알림방`을 지웠고 항목이 chatID가 아니면 첫 초기화 여부와 관계없이 hololive-api 기동을 거절합니다. `KAKAO_ROOMS`가 없거나 비면 기존 필수값 검증이 hololive-api(bot·admin plane)와 alarm-worker 기동을 거절하므로, 배포 전에 중앙 compose 보간 env의 `KAKAO_ROOMS`가 chatID 목록이어야 합니다. 멤버 목록 API(`GET /api/holo/members`)가 별명 없는 멤버에도 `aliases: {"ko":[],"ja":[]}`를 항상 내는 계약을 테스트로 고정했으며, iris-console은 이 계약에 맞춰 부재·null 보정을 지웁니다.
- LLM 지시를 invariant/developer/user 계층으로만 보냅니다(`DEC-20260926-stack-llm-instruction-layering-sole-path`, `DEC-20260926-stack-shared-go-compat-api-retirement`, T19). 내부 `llm.Client.GenerateJSON`이 `openaipreset.PromptLayers`를 받습니다. major event 요약 생성의 신뢰 경계(web_search_context 안의 지시는 무시하고 데이터로만 다룸)는 `prompts/invariant_prompt.tmpl`로 분리해 invariant 계층으로 보내고, major event·member news의 요약·검토·판정 작업 절차와 출력 형식은 developer 계층, 사건·후보·검색 결과는 user 계층입니다. prompt 자산 버전이 바뀌어 major event 요약 캐시(24시간)는 새 키로 다시 채워집니다. OpenAI 호환 client는 shared-go `JSONRequest.SystemPrompt` 대신 `InvariantPrompt`·`DeveloperPrompt`를 쓰고(Responses는 라벨 붙은 developer 메시지, Chat Completions는 라벨 붙은 단일 system 메시지), preset client는 퇴역할 `openaipreset.GenerateJSON(systemPrompt)` 대신 `GenerateJSONAs`로 JSON 원문을 받으며 schema 이름이 비면 생성에 실패합니다. Gemini의 `system_instruction`도 같은 라벨의 단일 섹션으로 보냅니다. 출력 유출 검사는 invariant·developer 계층을 함께 보호합니다. preset provider가 JSON이 아닌 출력을 내면 client 오류로 드러나며, member news consensus는 이전과 같이 primary 요약을 씁니다(로그 문구만 parse 실패에서 호출 실패로 바뀜).
- DB pool은 적재한 `POSTGRES_SSLROOTCERT`를 pgxdb `Config.SSLRootCert`로 직접 넘기고 shared-go pgxdb의 env 폴백에 기대지 않습니다(`DEC-20260926-stack-shared-go-compat-api-retirement`). 설정 원천과 값은 그대로이며, shared-go가 env 폴백을 지우기 전에 이 release가 먼저 배포되어야 합니다. youtube scraper 테스트는 퇴역할 `jsonutil.ErrBodyTooLarge` 대신 `httputil.ErrResponseBodyTooLarge`를 씁니다.
- native 완료 검사도 기존 bounded issuer 준비 대기를 사용해 SDK 기동 중의 단발 health 실패를 피합니다. 실제 5초 worker 기동에서 단발 검사는 2초에 실패하고 준비 대기는 성공했습니다. SDK 기동 deadline과 세대 교체 준비 deadline의 종료·정리 회귀를 추가하며 발급·upstream 예산은 변경하지 않습니다.
- issuer의 신뢰된 SDK import를 서비스 준비 단계로 옮겨 첫 prepare의 8초 예산에서 분리합니다. worker의 `loaded` 전에는 health를 제공하지 않고 native 배포/복원도 준비를 기다립니다. SDK 기동은 30초, helper의 세대 교체 준비는 종료·재시작을 포함해 40초로 제한합니다. 준비 뒤 prepare부터 발급 15초를 적용하며 개별 요청 8초·시도 간격 300초·격리 상한은 유지합니다. CPU 제한 재현과 실제 세대 교체 smoke를 통과했으며 운영 자원 경합 전반의 해결을 보장하지는 않습니다.
- PO 발급 실패 뒤 generation 정리 오류가 최초 원인을 덮어쓰던 진단 결함을 수정합니다. `last_error`와 `cleanup_error`를 분리하고, 새 발급 cycle에서 이전 오류를 초기화합니다. 요청·시간 상한과 재발급 간격은 변경하지 않습니다.
- 구독 대상 동시 조회 회귀를 `testing/synctest`로 검증해 전체 빌드 부하에서 100ms goroutine 시작 제한이 실패하던 문제를 제거합니다. 두 조회의 동시 진행과 타입별 반환 대상 검증은 유지하고 DB fixture는 가상 시계 밖에서 관리합니다.
- YouTube 라이브 확인에 정상 PO Token 발급·갱신·영상별 첨부를 연결합니다. 외부 interpreter는 앱 비밀과 네트워크가 없는 별도 issuer에서 실행하며, challenge 요청 전에 동일 UA/JSDOM 준비를 완료합니다. 만료·취소·발급 실패에는 기존 단일 무토큰 조회와 UNKNOWN 판정을 보존합니다.
- native/Compose collector와 issuer를 같은 full SHA로 빌드·검증·교체하고 실패 시 함께 복원합니다. AP 소스는 staging 검증 뒤 승격하고 snapshot으로 중단 전 상태를 복원합니다. 중앙 개별 collector/issuer 재배포는 paired entrypoint로 통합하며 `compose-redeploy-service.sh all`은 지원하지 않습니다.
- issuer rootfs의 소켓 디렉터리 권한(0770)과 `/tmp` sticky bit(1777)를 이미지 복사·일반 사용자 압축 해제에서도 보존합니다. 빌드 호스트 umask에 따라 배포 검증이 실패하던 원인을 수정하며 기존 무결성 검사는 유지합니다.
- native 배포의 두 필수 스크립트를 각각 검사해 정상 payload의 잘못된 거부를 고치고, 어느 파일이 빠져도 운영 변경 전에 중단합니다.
- native 설치·재검증·복원에서 root 소유 issuer archive의 hash는 root 권한으로 읽고 0600 보호 권한을 유지합니다. `systemd-analyze verify`에는 RootDirectory 안의 실제 실행 경로를 해석한 검사용 사본을 제공하며, 설치 unit의 격리 설정과 실행 파일 부재 거부는 보존합니다.
- systemd가 사용하는 빈 mountpoint를 issuer 이미지에 포함해 기동 전후 rootfs manifest를 동일하게 유지합니다. native 최초 설치의 rollback은 기록된 issuer 부재를 명시적으로 검증하고, 기존 bounded readiness 대기를 재사용해 재기동 직후 503을 완료로 오판하지 않습니다.
- Docker classic/containerd 저장소가 같은 이미지에 서로 다른 ID를 보고하는 차이를 archive의 manifest/config digest로 검증합니다. 등록된 두 표현 외의 이미지, revision·architecture·archive hash 불일치는 계속 거부합니다.
- 배포 검사에서 스크립트 문자열·등장 횟수·옛 함수 위치와 main 호출 배선만 비교하던 스냅샷을 제거하고, 버전 불일치·구성 실패·아티팩트 무결성·잘못된 배포 대상의 거부 검증은 유지합니다. 실제 broker CLI의 인수 거부·live socket 보존·강제 종료 뒤 stale socket 복구를 확인합니다.
- `youtube_live_absence_slots`를 `scheduled_for` 기준 30일(`YOUTUBE_PLANE_RETENTION_LIVE_ABSENCE_SLOTS_DAYS`, 기본 30, production은 양수 필수)이 지나면 retention tick당 최대 1000건 지웁니다. 삭제는 migration `221_live_absence_slot_retention.sql`의 제한된 함수가 수행하며, 30일보다 오래된 `live_snapshot`이 queue에서 대기·처리 중이거나 replay가 pending이면 보류합니다. 이미 session/head와 pending end에 반영된 사실은 대상이 아니지만, 30일 밖 slot은 과거 positive 재처리로 복원할 수 없습니다. 221은 2026-09-28 운영 ledger에 순번 082로 적용된 파일이라 bytes와 checksum을 바꾸지 않습니다.
- X가 로그아웃 상태의 `/home`을 로그인 화면으로 redirect하여 X 스페이스 수집이 요청 ID 초기화 단계에서 `collector_failed`로 반복 실패하던 문제를 수정합니다. `x-client-transaction-id`를 `0.3.2`로 올리고, helper 허용 목록을 인증 없는 앱 셸 `https://x.com/i/jf/`로 바꿉니다. 쿠키 전송 경로·오류 계약·런타임 설정은 변경하지 않습니다.
- X 스페이스 helper 실패 로그에 실패 단계(`input`·`library`·`app_shell`·`transaction`·`collect`)를 남기고, 분류되지 않은 `collector_failed`에는 내장 오류 종류와 Node 오류 코드만 덧붙입니다. helper가 결과 문서를 끝내지 못하면 worker가 관측한 종료 상태를 `helper_output`으로 기록합니다. 예외 원문·stderr는 계속 버리며 세션 상태의 오류 코드·재시도 간격은 바꾸지 않습니다. helper와 worker의 결과 문서 형식이 함께 바뀌므로 alarm worker 이미지 단위로 배포합니다.
- live consume의 현재 absence slot 재조회에 `scheduled_for` 인덱스(migration 212)를 추가합니다. 채널 GIN이 해당 채널의 무기한 보존 이력 전체를 heap에서 다시 검사하던 비용을 slot 한 행으로 줄입니다. absence slot 보존 계약과 조회 결과는 바꾸지 않습니다.
- 운영 조회가 선택하지 않는 source observation·queue 인덱스 네 개를 `DROP INDEX CONCURRENTLY`로 제거합니다(migration 213–216). 보존 기간과 보존 삭제·claim 경로는 유지합니다.
- 로컬 통합 검사가 PostgreSQL·Valkey 일회용 컨테이너를 지울 때 이미지가 선언한 익명 volume도 함께 지웁니다.
- 중앙 compose wrapper의 release version export 검사를 gate에 연결된 `ap-deploy-version_test.sh`로 옮깁니다. `compose-version-contract_test.sh`는 어떤 gate에도 연결되지 않은 채 2026-09-14 admin 통합 이후 Compose 문구 개수 불일치로 실패해 왔으므로 삭제합니다. 두 VERSION 읽기와 호출자 버전 불일치의 fail-closed는 계속 검사합니다.
- YouTube 발송 정리에서 FAILED 자식에 존재하지 않는 ledger 증거를 요구해 terminal outbox가 영구 보류되던 문제를 수정합니다. 같은 논리 키의 PENDING/SENDING 자식은 계속 보류하고, SENT/QUARANTINED 자식의 ledger 검증도 유지합니다.
- alarm dispatch 보존 삭제가 행 잠금을 기다리는 동안 DLQ 행이 retry로 재등록되면 삭제하지 않도록 상태·보존 시각을 다시 검사합니다. 수동 보존 삭제 스크립트에도 같은 조건을 적용합니다.
- YouTube PENDING claim·stale SENDING 조회에 상태 리터럴을 명시해 generic plan에서도 부분 인덱스 조건을 증명하고, aggregate sync 후보는 EXISTS/NOT EXISTS로 조회합니다. 후보 집합·순서·배치 상한은 유지합니다.
- 운영 호출자가 없는 `alarmread.Reader`, `ProvideAlarmReader`, `alarm.Repository.GetAllChannelIDs`와 대응 SQL을 제거합니다. YouTube plane의 projection transaction과 llm plane membernews의 직접 구독 조회는 유지합니다.
- source observation claim 후보를 PENDING과 만료 PROCESSING 가지로 나눕니다. PENDING 가지는 partial index 순서로 LIMIT까지만 읽고, 만료 PROCESSING 가지는 만료 행만 정렬합니다. 활성 backlog 5만 행 fixture에서 후보 선택이 읽는 행이 약 9.8만에서 10행 안팎으로 줄고, claim 순서·`SKIP LOCKED`·replay epoch·shorts 순서 계약은 유지합니다. 한 claim은 최대 2×LIMIT행을 claim transaction이 끝날 때까지 잠급니다.
- 발행 검증(fence·projection·target) 조회 세 개를 pgx 파이프라인 한 번으로 보냅니다. 잠금 순서와 판정 우선순위는 그대로이며 발행당 왕복이 두 번 줄어듭니다.
- content 관측은 로드 값과 달라진 evidence clock만 한 번의 배치로 저장하고, clock upsert에 값 열 14개의 `IS DISTINCT FROM` 가드를 둡니다. 상태 로드는 관측된 영상·clock 보유 영상과 현재·이후 absence slot만 잠그며 slot을 `scheduled_for` 순으로 적용합니다. 역순으로 저장된 slot을 sequential scan으로 읽어도 첫째·둘째 부재와 철회 시각이 유지되는지 PostgreSQL 회귀 테스트로 검증합니다.
- schedule 관측이 읽지 않던 누적 schedule item 전체 `FOR UPDATE` 조회를 제거하고, claim 예산은 남은 lease가 예산보다 짧을 때만 연장합니다. live 종료 finalizer는 due 조회에서 DB 시각을 함께 읽고, content 충돌 기록은 공용 reconcile 충돌 SQL을 씁니다. migration은 없습니다.
- 커뮤니티 게시물을 다시 관측했을 때 좋아요·댓글 수와 `published_at` 보강값이 그대로면 행을 다시 쓰지 않습니다. 따라서 `youtube_community_posts.last_seen_at`은 마지막 관측 시각이 아니라 마지막 값 변화 시각을 뜻하며, 이 열을 읽는 곳은 없습니다.
- 발송 telemetry의 기록 대상 선택과 lease 획득을 `FOR UPDATE SKIP LOCKED` 한 문장으로 합칩니다. 다른 인스턴스가 잡은 행은 기다리지 않고 건너뛰므로 한 번에 배치 한도보다 적게 반환할 수 있습니다. 반환 순서는 `(event_at, id)`입니다.
- bot 원장 보존 삭제의 cutoff를 문장 시작 시각(`statement_timestamp()`) 기준으로 계산하고 outbox 조건을 부분 인덱스 술어별로 나눠, 보존 기간 안의 이력을 훑지 않고 만료 행만 terminal 부분 인덱스로 읽습니다. 보존 기간, manual_review 분리 보존, 배치 한도와 `SKIP LOCKED`는 그대로입니다.
- Kakao 방 정보가 이미 저장값과 같으면 명령마다 하던 upsert 쓰기 트랜잭션을 만들지 않습니다. collector job lease 획득은 claim UPDATE가 돌려준 식별자로 job identity를 검증해 같은 행을 다시 읽는 왕복 한 번을 없애며, 불일치는 계속 `ErrInvalidJob`으로 롤백합니다.
- 호출자가 없는 hololive-shared·API 코드를 삭제합니다: `ViewerSampleCleaner`와 viewer 표본 보존 삭제 SQL, `dbx.WithSessionAdvisoryLock`, 발송 telemetry의 로그·경로 사용량·채널 게시물 요약·지연 기간 요약 조회, 게시물 타임라인의 기간 조회 두 개와 발송 수의 게시 구간 조회, ACL `CountRooms`. viewer 표본 데이터와 스키마는 DEC-20260925에 따라 유지하며 migration은 없습니다.
- `POSTGRES_POOL_MIN_CONNS=0`을 idle 연결 없음으로 그대로 적용합니다. 이전에는 plane 검증이 허용한 0을 풀 생성 시 2로 바꿔 `MIN=0·MAX=1` 조합이 기동에 실패했습니다. 음수는 연결 전에 거부합니다. 운영 compose 기본값(MIN 1·2)에는 영향이 없습니다.
- PostgreSQL 용량 gate의 reserve를 superuser 예약 3(`@superuser-reserved`)을 뺀 비슈퍼유저 여유로 계산하고 하한을 2로 둡니다. 현재 할당 55에서 통과·거부 판정은 이전과 같으며, compose가 `superuser_reserved_connections`를 바꾸면 policy 불일치로 거부합니다. collector 기본 max(8)는 바꾸지 않습니다.
- `holo-postgres`에 `log_autovacuum_min_duration=10s`를 추가해 10초 이상 걸린 autovacuum의 WAL/FPI·소요시간을 로그로 남깁니다. compose command 값이라 `holo-postgres` 재생성 뒤에 적용되며, 재생성은 별도 운영 승인으로 수행합니다. 전체 compose `up`뿐 아니라 서비스별 배포 wrapper의 선행 `run --rm hololive-db-migrate`도 의존 DB를 재생성할 수 있으므로, 최종 앱 `up --no-deps`만 보고 DB 무중단 배포로 판단하지 않습니다.
- 참조가 없거나 실행하면 해로운 수동 SQL(`seed_member_celebration_dates.sql`, `audit_message_contract_087_090.sql`, 주석뿐인 `pg18_db_usage_optional_concurrent_indexes.sql`)과 PK 때문에 항상 0행인 점검 쿼리, 호출자가 없는 `dbx` 배치 삭제 helper를 삭제합니다. dbtest 하니스는 러너와 같은 `dbmigrate.Manifest`로 manifest를 해석합니다.
- `.env.example`의 YouTube plane 보존 주기 예시값을 코드 기본값·DEC-20260824와 같은 120초로 맞춥니다. 300초에서는 보존 삭제 상한(1회 1,000행)이 하루 28.8만 행이라 2026-09-27에 실측한 application 유입(하루 약 42만 행)을 따라가지 못합니다. 운영 `compose.env`는 같은 날 120초로 바꿨습니다.

## v4.0.1 - 2026-09-25

- 공유 모듈을 `shared-go v2.7.1`, gRPC를 수정 안정판 `v1.83.2`로 고정하여 `CVE-2026-84445` / `GO-2026-6443`을 해소합니다. 정기 보안 검사의 shared-go SHA도 같은 릴리즈에 맞춥니다.
- ARM64 이미지 검사에서 `grpc v1.84.0`의 `affected` finding을 통과시키던 예외를 제거하고, 정확한 패키지 부재 증명만 허용합니다.
- GitHub Release 제목·본문을 `v3.5.0`의 자동 생성 형식으로 통일하고, 운영 검증 기록과 공개 릴리즈 본문의 작성 절차를 분리합니다.
- 운영 산출물은 API·collector `4.0.1`, alarm worker `3.2.6`으로 식별합니다. DB migration·런타임 설정·공개 API는 변경하지 않습니다.

## v4.0.0 - 2026-09-25

- YouTube.js 시청자 수 전용 RPC와 Holodex의 viewer 표본 발행을 중단합니다. 방송 상태·일정·알림 및 과거 관측의 소비·재처리는 유지합니다.
- 라이브 명령과 템플릿 미리보기에서 `ViewerCount`를 제거하고 미지원 변수는 명시적으로 거절합니다. 방송 개수·제목·링크와 외부 Stream API 필드는 보존합니다.
- Holodex 작업별로 필요한 payload만 계산하여 무관한 메타데이터 충돌이 방송·일정 수집을 막는 결함과 중복 순회·복사를 제거합니다.
- 수집 수요를 네 YouTube.js 작업으로 집계하고 방송 상태 진단을 viewer 대상에서 분리합니다. migration 210의 활성 head 부분 인덱스로 종료 이력 전체 조회를 피합니다.
- `ViewerCount` 템플릿 변수와 viewer 운영 지표의 계약 퇴역을 포함하므로 root/API artifact를 4.0.0으로 올립니다. alarm-worker 3.2.5와 외부 Stream API는 유지합니다.
- 관리자 기능 조회의 취소 신호를 HTTP까지 전달하고, 사전 로딩 실패와 화면 코드 복구를 정리했습니다.
- 이름·채널 ID 저장 중 추가 입력을 잠가 저장 응답이 새 초안을 닫아 버리지 않도록 했습니다.
- 멤버와 통계 전용 UI를 각 기능 디렉터리로 옮겨 소유 경계를 일치시켰습니다.

## v3.5.6 - 2026-09-24

- 템플릿 표시·복사 가능한 명령과 DB row_version 기반 캐시 일관성을 적용합니다(migration 203–208).
- 수집 대상·진행·RPC 지표를 추가하고 unknown을 보존하는 발송 전이를 필수화합니다.
- 퇴역 X Spaces 자동 로그인 실행 경로를 제거하고 수동 쿠키와 읽기 전용 이력은 유지합니다.
- API 3.2.4, alarm worker 3.2.5와 shared-go v2.7.0을 사용합니다.

## v3.5.5 - 2026-09-24

### 수정

- 게시된 shared-go v2.6.4와 iris-client-go v2.6.2를 모든 Go 소비 모듈에 고정하고 QUIC 0.63.0 및 OpenAI 클라이언트 패치를 적용했습니다.
- YouTube.js 18.1.0과 undici 8.11.0을 고정하고 업스트림에서 해결된 attachment-run 보정 코드를 제거했습니다.
- 앞서 main에 병합된 YouTube 429 cooldown 수정을 중앙 collector 이미지에 포함합니다.
- 중앙 Valkey 9.1.2의 Alpine 3.24.2 이미지를 검증된 digest로 고정하고, CI Python 실행기와 uv 부트스트랩을 0.12.18로 정렬했습니다.
- X Spaces 로그인 이미지는 Chromium headless shell만 보존하도록 재구성하고 Node 24.21.0을 사용합니다.
- 이번 릴리즈의 API artifact 버전은 3.2.3, alarm-worker artifact 버전은 3.2.4입니다.

## v3.5.4 - 2026-09-12

### 수정

- iris-client-go v2.6.0을 소비 모듈에 고정해 API webhook 생성 시 HandlerOption을 한 번만
  적용하며 기존 인증·nonce 저장소·durable admission 설정을 보존합니다.
- YouTube delivery 전이가 요청한 방·알림 종류·identity tuple만 묶어 다른 요청의 교차 조합을
  선택하지 않습니다. Raw·canonical identity 후보를 한 번씩 join하고 direct ID와 합쳐 중복을
  제거하며 기존 정렬·조회 상한·행 잠금 계약을 유지합니다.
- 실제 prepared custom/generic 계획에서 tuple-only sibling과 direct-only 행의 전체 ID·순서,
  요청 관계의 반복 실행 상한을 회귀 검사합니다.
- 배포 전 PG hot-path 검사가 YouTube claim의 `FOR UPDATE OF outbox SKIP LOCKED` 구문을
  포함하고, EXPLAIN·관측기·혼합 fingerprint를 제외하도록 SQL·결과 분류를 일치시킵니다.
- 이번 릴리즈의 API artifact 버전은 3.2.2, alarm-worker artifact 버전은 3.2.3입니다.

## v3.5.3 - 2026-09-11

### 수정

- alarm-worker 3.2.2의 LIVE 발송 누락 검사가 알림 생성과 동일하게 확인된 Premiere를 제외합니다.
  병합 후 분류도 반영하며 일반 LIVE의 발송·전달 누락 감지는 유지합니다.

## v3.5.2 - 2026-09-07

### 수정

- 알림 전송을 일반 텍스트 경로로 맞추고 shared-go v2.5.2로 URL·코드 원문을 보존합니다.
- fanout claim의 pending partial index 조건을 명시하고 generic/custom plan·잠금 행 회귀를 검증합니다.
- API와 alarm-worker artifact 버전을 3.2.1로 올리고 동일 source revision으로 빌드합니다.
- YouTube.js helper는 소켓 파일 생성과 listen 준비를 구분해 bootstrap 전송 전 기동 경합을 막습니다.

## v3.5.1 - 2026-09-04

### 수정

- Host-native AP 배포가 호출자의 restrictive umask와 무관하게 root 소유 YouTube.js helper와
  정적 데이터에 서비스 계정의 읽기·순회 권한을 부여합니다.

## v3.5.0 - 2026-09-04

### 추가

- YouTube delivery ledger lifecycle과 source-observation replay epoch 기반을 추가합니다.
  이번 릴리스는 epoch activation과 historical backfill을 실행하지 않습니다.
- 확인된 일반채팅의 YouTube-addressable 알림을 Karing content-list로 보내고 오픈채팅은 기존
  Markdown lane을 유지합니다.

### 수정

- Alarm-worker는 Iris `202 Accepted` 뒤 exact reply status의 `handoff_completed`를 확인한
  경우에만 Karing 성공으로 처리합니다. Egress path를 envelope당 한 번 결정해 split group 전체에
  고정하고 결과 불명확 상태는 재발송하지 않습니다.
- YouTube collector가 방송 예정 시각 metadata를 복구하고 lifecycle 전환을 한 owner에 결속합니다.
- `x/crypto`와 `fast-uri` 보안 권고를 해소하고 Node 24.20.0 production image와 npm dependency
  provenance를 갱신합니다.

### 변경

- migration 190·191이 delivery table constraint와 ledger/replay 기반을 추가합니다. Migration
  one-shot은 종료 코드 0을 직접 전파하며 central cutover는 build host와 runtime no-build 단계를
  분리합니다.
- Go toolchain을 `1.27.1`로 올리고 여섯 Go module 및 두 npm application dependency graph를
  갱신합니다. 모든 consumer는 `iris-client-go v2.4.2`, `shared-go v2.5.0`에 고정합니다.
- 전체 source release는 `3.5.0`, hololive-api·alarm-worker·collector·admin·migrator artifact는
  `3.2.0`입니다.

## v3.4.0 - 2026-08-29

### 추가

- celebration과 birthday-stream event identity에 stable `MemberID`를 포함하고, rollout 기간에는
  legacy payload가 member name·kind·channel·date·video와 일치할 때만 audience, payload와
  daily cap을 이관합니다. 같은 channel을 공유하는 다른 member는 legacy event를 재사용하지
  않습니다.
- YouTube collection lease에 bounded failure code/class/detail/timestamp를 추가하고 migration 189가
  legacy deferred row를 5,000행씩 reconcile합니다. `shutdown_release`는 failure로 기록하지 않으며
  constraint validation과 checksum reconciliation은 재실행에 안전합니다.
- admin dashboard public ingress를 dedicated nftables/systemd owner와 rendered nginx allowlist에
  결속합니다.

### 수정

- alarm dispatch event-hash collision은 collision ledger를 기록한 뒤 canonical winner delivery를
  계속 처리하며, claim release와 terminal error persistence가 ownership token과 cleanup context를
  보존합니다.
- YouTube collector, outbox, member cache, notification cache와 template renderer의 bounded
  cleanup·privacy key·fallback 경계를 정렬하고 invalid/ambiguous 상태를 success로 강등하지
  않습니다.
- bot ingress durable admission과 RSS/major-event parsing이 malformed row를 격리하면서 정상 row와
  이미 commit된 결과를 보존합니다.

### 운영

- PostgreSQL TLS/HBA와 route failover rollback, central/Seoul Compose, Osaka/Osaka2 host-native AP
  배포 계약을 정렬했습니다. AP deploy는 실제 rsync input manifest를 cutover 전에 검증하고
  실패 시 이전 route와 artifact를 복구합니다.
- CI ownership gate가 app workflow 전체를 module-specific SHA-256 snapshot으로 고정해 early
  exit, folded/flow YAML, custom shell, environment injection과 필수 gate 삭제를 거부합니다.
- GitHub Actions는 공식 `setup-python`과 고정된 `uv 0.12.7` 설치 경로를 사용하며,
  `shared-go v2.1.0`과 `iris-client-go v2.3.1`을 채택합니다.
- 이번 릴리스는 root app `3.4.0`, `hololive-api`·`youtube-collector`·`admin-dashboard` artifact
  `3.1.0`, `hololive-alarm-worker` artifact `3.1.0`입니다.

## v3.3.4 - 2026-08-26

### 변경

- Go 1.27 관용구 정렬과 병렬 게이트 안정화, 명령 파서 benchmark를 전 runtime 모듈의
  검증된 source snapshot에 포함했습니다.
- 모든 production Go 모듈의 `shared-go`·`iris-client-go` pin을 각각 `v2.0.4`·`v2.2.3`으로
  정렬하고 security workflow도 같은 exact commit을 사용하도록 갱신했습니다.
- 이번 릴리스는 root app `3.3.4`, `hololive-api`·`youtube-collector`·
  `admin-dashboard` artifact `3.0.12`, `hololive-alarm-worker` artifact `3.0.8`입니다.

## v3.3.3 - 2026-08-26

### 변경

- `admin-dashboard`, `hololive-alarm-worker`, `hololive-dbtest`, `hololive-shared`,
  `youtube-collector`의 production `shared-go` pin을 `v2.0.3`으로 정렬하고,
  security workflow의 `shared-go`·`iris-client-go` checkout도 각각 `v2.0.3`·`v2.2.2`
  exact commit으로 맞춰 로컬과 CI의 production dependency 경계를 일치시켰습니다.
- 이번 릴리스는 root app `3.3.3`, `hololive-api`·`youtube-collector`·
  `admin-dashboard` artifact `3.0.11`, `hololive-alarm-worker` artifact `3.0.7`입니다.

## v3.3.2 - 2026-08-26

### 수정

- YouTube.js가 알려진 transient network 오류와 안전한 Innertube read-only POST의
  `500`·`502`·`503`·`504`만 정확히 한 번 재시도합니다. `429`, 다른 endpoint·method,
  재생 불가능한 body, parser·protocol 오류와 두 번째 실패는 재시도하지 않습니다.
- shared H3 인증서 reloader와 `shared-go v2.0.3`, `iris-client-go v2.2.2`를 채택해 인증서
  재적재와 취소된 H3 resolve 경계를 최신 fail-closed 계약으로 정렬했습니다.

### 변경

- 이번 릴리스는 root app `3.3.2`, `hololive-api`와 `youtube-collector` artifact `3.0.10`이며
  변경되지 않은 alarm-worker version은 유지합니다.

## v3.3.1 - 2026-08-26

### 수정

- YouTube 커뮤니티 관측 창의 정렬이나 고정 글 변화가 기존 글 전체를 신규 글로 오인해 알림을
  폭주시키던 문제를 고쳤습니다. canonical post ID로 신규성을 판정하고 최초 관측은 기준선만
  생성합니다.

### 변경

- `hololive-api`의 production lifecycle 소유권을 Fx `v1.24.0`으로 옮겼습니다. 기존 build·start·
  shutdown 순서, fatal 우선순위, drain timeout과 정확히 한 번의 cleanup 계약은 유지합니다.
- Structure Gate v2를 legacy hard baseline 없이 적용했습니다. 이번 릴리스는 root app `3.3.1`,
  `hololive-api`와 `youtube-collector` artifact `3.0.9`이며 alarm worker version은 유지합니다.

## v3.3.0 - 2026-08-25

### 변경

- Go toolchain과 builder 기준을 `1.27.0`으로, `golangci-lint`를 `v2.13.1`로, `staticcheck`를
  `2026.2.1`로 올렸습니다. Go 1.27 `go fix` modernizer 재작성(`errors.AsType`, `sync/atomic` 타입,
  `slices.Backward`, 내장 필드 리터럴)을 적용했고, `apperrors.ServiceError`는 생성자·소비자와 같은
  포인터 리시버로 통일해 `%w` 포장이 Go 1.27 vet 검사를 통과합니다. request id·delivery lock
  토큰·admin 사용자 id 생성은 `github.com/google/uuid` 대신 Go 1.27 표준 `uuid`를 사용합니다.
- YouTube 최초공개 메타데이터를 source observation, reconciliation, outbox까지 보존하고 발송 시점의
  일정으로 남은 분을 다시 계산합니다. 예정된 최초공개는 `N분 후 공개 예정`, 시작했거나 일정이 없는
  최초공개는 `최초공개`, 일반 업로드는 `새 영상`으로 렌더링합니다. Karing 문자열과 기본 텍스트
  템플릿은 migration `188_youtube_premiere_notification_labels.sql`로 관리합니다.
- Holodex API 클라이언트가 주입된 표준 HTTP transport에도 HTTP/2 전용 정책을 적용하도록 고쳐,
  상류가 HTTP/2 SETTINGS frame을 보낼 때 HTTP/1.x malformed response로 오인하던 운영 장애를
  제거했습니다. 커스텀 RoundTripper 주입 계약은 변경하지 않았습니다.
- immutable epoch2 baseline의 checksum을 운영 ledger와 다시 일치시키고, 비동기 YouTube.js 페이지
  매퍼의 선언 타입을 실제 `paginate` 계약과 정렬했습니다.
- 호환 범위 의존성을 갱신했습니다. Go 직접 의존성은 `testify 1.12.1`, `gofeed 1.4.2`,
  `openai-go/v3 3.52.0`으로 올렸고, tidy 결과에는 MongoDB driver `2.8.1`, logrus `1.10.1`,
  gRPC `1.83.1`과 같은 transitive 갱신이 반영됩니다. 관리자 frontend는 TanStack Query
  `5.102.3`, React Virtual `3.14.10`, lucide-react `1.34.0`, Node types `26.3.0`, React DOM
  types `19.2.5`, Vite React plugin `6.1.0`, ESLint `10.9.1`, typescript-eslint `8.68.0`,
  Vite `8.2.2`로 올렸고 YouTube.js helper도 Node types `26.3.0`으로 정렬했습니다.
- 이번 릴리스는 root app `3.3.0`, `hololive-api` `3.0.8`, `hololive-alarm-worker` `3.0.6`으로
  올립니다. `youtube-collector` artifact version은 `hololive-api` VERSION을 따릅니다.

## v3.2.1 - 2026-08-21

### 수정

- `scripts/deploy/materialize-admin-dashboard-secrets.sh`에 실행 권한을 부여했습니다.
  v3.2.0에서 이 스크립트가 `100644`로 커밋되는 바람에, `systemd-compose-up.sh`가 직접
  실행하는 지점에서 `Permission denied`(exit 126)로 기동이 멈췄습니다. 중앙 호스트
  배포에서 재현했으며, 실행 중인 컨테이너는 영향을 받지 않고 기동만 실패합니다.

## v3.2.0 - 2026-08-21

### 변경

- 관리자 대시보드 세션을 안정적인 family 단위로 다시 묶었습니다. Valkey에 family lease를
  두고 회전을 family 인식·수렴형으로 바꿔, 동시 회전에서도 승자가 하나로 정해지고 이미
  업그레이드된 WebSocket까지 family 단위로 취소됩니다. 세션당 스트림 상한은 토큰 회전을
  건너도 유지됩니다.
- 로그인 실패 예산을 프로세스 로컬에서 Valkey 기반 분산 카운터로 옮기고 IP·계정·전역 세
  축으로 나눴습니다(15분 창, 각각 10·30·200회). Valkey 클라이언트는 `DisableCache`·
  `ForceSingleClient`로 고정해 카운터가 클라이언트 캐시나 다중 연결로 흩어지지 않습니다.
- 세션 서명 키와 CSRF 서명 키에 도메인 분리를 적용해, 한쪽 토큰을 다른 쪽 검증에 재사용할
  수 없게 했습니다.
- 운영 시크릿을 `*_FILE` 경로로만 읽도록 바꾸고 `SESSION_SECRET`에 32바이트 하한을
  강제합니다. 읽기 경로는 `Lstat` → `Open` → `os.SameFile` 대조로 TOCTOU를 막고,
  환경 변수 복원 실패를 삼키지 않고 오류로 전파해 시크릿이 남은 채로 기동하지 않습니다.
  Docker `Config.Env`에는 경로만 남습니다.
- CSP와 브라우저 권한 정책을 강화한 미들웨어를 필수 경로로 만들고, Docker 소켓 접근을
  최소 권한 프록시로 격리했습니다. 배포는 `deploy/compose/docker-compose.admin-security.yml`
  오버레이를 기동 시 필수로 요구하며, `scripts/deploy/materialize-admin-dashboard-secrets.sh`가
  `*_FILE` 시크릿을 안전한 권한으로 준비합니다.

## v3.1.0 - 2026-08-21

### 변경

- 적대적 감사에서 확정된 결함 6건을 수정했습니다.
  - 분류되지 않은 상류 오류가 fatal로 승격되어 수집기 프로세스 전체를 정지시키던 문제를
    고쳤습니다. `FromContext`의 catch-all이 만들어낸 typed 오류와 진짜 분류를 구분하는
    미분류 표식을 도입하고, fatal 승격에서 미분류를 제외합니다.
  - 템플릿 저장의 UPSERT와 이전 본문 revision 기록을 한 트랜잭션으로 원자화했습니다.
    이전에는 본문만 교체되고 revision이 남지 않아 이 API로는 롤백할 수단이 없었습니다.
    동시 저장 시 revision 순서가 뒤집히지 않도록 기록 시각을 잠금 획득 시점에 맞춥니다.
  - 알람 발송에서 만료된 attempt 컨텍스트로 상태를 기록하려다 실패해, 드레인된 행이
    `sending`으로 남아 재시도 대신 terminal quarantine으로 굳던 문제를 고쳤습니다.
    상태 기록과 실패 라우팅은 발송 attempt와 분리된 컨텍스트에서 완료됩니다.
  - 요청 ID의 봉투 수 상한이 재시도 허가 판정과 어긋나 같은 알람이 두 번 발화할 수 있던
    중복 구현을 제거했습니다.
  - Holodex 응답에서 row 하나가 어긋나면 정상 row 전체가 폐기되고 같은 파서를 공유하는
    live·viewer·channel_stats·photo·schedule 작업이 동시에 멈추던 문제를 고쳤습니다.
    같은 저장소 `officialcollector`와 동일하게 불량 row는 건너뛰고 전부 불량일 때만
    실패합니다.
  - lease 획득이 in-flight 완료 트랜잭션 뒤로 직렬화되던 잠금 순서를 교정했습니다.
    SQL 변경 없이 호출 순서만 바꿔, 획득 불가 상태에서 완료 커밋을 기다리지 않습니다.
- `shared-go`를 `v1.54.0`으로, `iris-client-go`를 `v2.1.3`으로 올렸습니다.

## v3.0.7 - 2026-08-21

### 변경

- YouTube plane의 `queue_observability.sql`이 `status IN (...)` 대신 `OR` 조건으로 `source_observation_queue`의
  partial index 경로(BitmapOr)를 타고, `observePendingQueue`의 DB 조회는 5초 간격으로 제한됩니다(utilization
  게이지는 매 호출 갱신). 운영 EXPLAIN에서 Seq Scan이 Bitmap Index Scan으로 바뀌는 것을 확인했습니다. (#402)
- youtubejs helper가 youtubei.js 18.0.0 `Text.fromAttributed`의 `length` 없는 attachment run 미매칭을
  상류 PR LuanRT/YouTube.js#1241과 같은 정규화 shim(`youtubei-attachment-run-fix.mjs`)으로 선적용해, 채널 페이지마다
  남던 `[YOUTUBEJS][Text]` 경고와 객체 덤프의 원인을 제거합니다. canary 테스트가 상류 수정이 반영되면 실패해
  shim 제거 시점을 알립니다. (#402)
- 이번 릴리스는 `hololive-api`(3.0.7)와 `youtube-collector` artifact를 재빌드합니다. `hololive-alarm-worker`(3.0.5)는
  변경이 없어 재빌드하지 않습니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

## v3.0.6 - 2026-08-21

### 변경

- 마이그레이션 `183_postgres_idle_transaction_timeout.sql`(manifest 순번 `044`)이 데이터베이스 기본값
  `idle_in_transaction_session_timeout = 5min`을 설정해, 오래 열린 idle transaction이 VACUUM horizon을
  붙잡아 `source_observation_queue`·`youtube_collection_job_leases`의 dead tuple 회수를 지연시키는 경로를
  막습니다. MVCC 운영 증적 문서를 함께 확장했습니다. (#376)
- 모든 Go 모듈의 `iris-client-go`를 `v2.1.2`로 갱신했습니다(내부 `randomhex` 도달 불가 분기 제거, 동작 동일).
- 이번 릴리스는 `hololive-api`(3.0.6)·`hololive-alarm-worker`(3.0.5)·`youtube-collector` artifact를 함께
  재빌드합니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

## v3.0.5 - 2026-08-20

### 변경

- settings가 `ALARM_DISPATCH_RETENTION_{INTERVAL_MS,QUERY_TIMEOUT_MS,LIMIT,SENT_DAYS,DLQ_DAYS,QUARANTINED_DAYS,CANCELLED_DAYS,EVENT_DAYS}`,
  `SCRAPER_SCHEDULER_WORKER_COUNT`, `SCRAPER_POLL_{VIDEOS,SHORTS,COMMUNITY,STATS,LIVE}_INTERVAL_SECONDS`의
  잘못된 명시 값(빈 문자열·비정수·0·음수)을 더 이상 기본값으로 되돌리지 않고 config 로딩 오류로
  거절합니다. 미설정은 기존 기본값 그대로입니다. `positiveIntEnv`·`secondsEnv`·`positiveDurationMS`
  helper를 제거하고 기존 `required*` 파서로 통일했습니다.
- live catch-up 억제가 cache 오류·깨진 marker에서 fail-open으로 동작하는 횟수를
  `hololive_youtube_outbox_live_catchup_suppression_total{result}`로 노출합니다. 동작 방향은 그대로입니다.
- messagestrings Store가 DB 로딩 실패와 키 부재를 `hololive_messagestrings_load_failures_total`,
  `hololive_messagestrings_lookup_fallback_total{reason,namespace}`로 구분해 노출합니다. 호출자가 없던
  `GetOr`를 제거했습니다.
- 운영 경로가 쓰지 않던 `alarmservice.AlarmService.WasUpcomingEventNotifiedRecently`와
  `alarmcache.State.WasUpcomingEventNotifiedRecently`(cache 오류를 `false`로 접던 래퍼)를 제거했습니다.
  upcoming 중복 판정은 `dedup.Service`가 계속 담당합니다.
- 이번 릴리스는 `hololive-api`·`hololive-alarm-worker`·`youtube-collector` artifact를 함께 재빌드합니다.
  `hololive-api` VERSION은 `v3.0.5`와 일치시키고, `hololive-alarm-worker` artifact version은 `3.0.3`에서
  `3.0.4`로 올립니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

## v3.0.4 - 2026-08-20

### 변경

- migration runner가 epoch-2 legacy ledger 계약(136건 체크섬 embed와 기동 시 검증)과
  baseline 체크섬 backfill 허용 경로를 더 이상 갖지 않습니다. `182_epoch2_legacy_ledger_cleanup.sql`이
  legacy ledger 행 136건을 `schema_migrations`·`schema_migration_checksums`에서 정리하고, 정리가
  적용된 뒤에도 manifest 밖 행이 남으면 기동을 거부합니다. 체크섬 목록은 M1 게이트 데이터로
  `scripts/architecture/`에 둡니다. (#396)
- YouTube collector loader와 Compose에서 HC-013 compat alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`,
  `YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`)를 제거했습니다. canonical 키만 읽습니다. (#394)
- settings 검증 helper 통합, youtubedispatch 파일 재편과 nil-fallback 접근자 제거, collector 실행
  파이프라인 `collectionExecutor` 추출, sourceobservation publish preflight 단일화 등 동작 동등
  구조 정리를 반영했습니다. (#393) 사후 리뷰에서 확인한 테스트 공백을 보강했습니다. (#395)
- 이번 릴리스는 `hololive-api` artifact만 재빌드하므로 해당 VERSION만 `v3.0.4`와 일치시켰습니다.
  `hololive-alarm-worker` artifact version은 `3.0.3`을 유지합니다.

### 수정

- dbtest가 postgres 컨테이너 제거 레이스에서 재시도하도록 수정했습니다. (#392)
- security workflow의 dependency checkout pin을 정렬했습니다. (#391)

## v3.0.3 - 2026-08-20

### 수정

- YouTube collector의 별도 metrics server에도 role-owned worker registry를 연결해
  `iris_stack_worker_*`가 실제 scrape endpoint에 노출되도록 수정했습니다.

### 변경

- `shared-go v1.53.0`으로 갱신하고 제거된 `workerconfig` package를 허용 목록에서도
  삭제했습니다. worker profile과 metrics는 strict `workercontract.Registry`만 소유합니다.
- 함께 재빌드하는 `hololive-api`와 `hololive-alarm-worker` artifact version을 repository
  release `v3.0.3`과 일치시켰습니다.

## v3.0.2 - 2026-08-20

### 수정

- Compose AP의 `youtube-collector-b`와 `youtube-collector-d`가 collector-c에서 상속한
  `STACK_WORKER_PROFILE_FILE`을 유지해 자신이 mount한 role profile을 찾지 못하던 identity
  회귀를 수정했습니다. AP 렌더가 instance ID와 같은 profile 경로를 환경변수와 read-only
  mount에 함께 사용하는지 검증합니다.

## v3.0.1 - 2026-08-20

### 수정

- central live-compat의 `volumes: !override`가 API, alarm-worker, collector-c의 role별
  Stack Worker Profile bind mount를 누락해 v3 runtime이 `profile_file_missing`으로
  기동하지 못하던 배포 회귀를 수정했습니다. 두 central Compose 조합에서 세 mount의
  source, target, read-only 속성을 렌더 결과로 검증합니다.

## v3.0.0 - 2026-08-20

### 추가

- central API·alarm-worker와 collector a/b/c/d에 role별 strict Stack Worker Contract v1
  profile을 도입했습니다. 실제 PostgreSQL/process queue와 executor를 `/diagnostics/workers` 및
  `iris_stack_worker_*`로 노출하고 worker enablement·capacity·timeout의 env 이중 소유를 제거했습니다.
- `iris-client-go/v2 v2.1.1`을 pin하고 bot webhook receiver를 HMAC v3-only 계약으로
  전환했습니다. nonce replay 방지는 명시적 Valkey store를 사용하며 v2 성공 metric을
  제거했습니다.
- YouTube collector의 provider 실패 class와 lease 실패 진단을 durable schema에 보존합니다.
  기존 deferred 실패를 backfill하고 acquire 경합 및 migration 중에도 진단이 유실되지 않도록
  trigger와 constraint 설치·검증 순서를 고정했습니다.

### 호환성이 깨지는 변경

- htmlscraper의 production `NewTestServiceWithHTTPClient`와 `*ForTest` accessor를 제거했습니다.
  custom YouTube/HTTP client가 필요한 구성은 `NewServiceWithDependencies`와
  `ServiceDependencies`를 사용하며, 기존 `NewServiceWithYouTubeClient`와
  `NewServiceWithOfficialSchedule` signature는 유지합니다.

### 변경

- standalone YouTube scrape/outbox runtime 모듈을 제거하고 AP a/b/c/d identity를 `youtube-collector` fleet로 통일했습니다. Canonical consume/notification/retention은 `hololive-api` YouTube plane, `members.photo`는 admin PhotoSync, egress는 `alarm-worker`입니다. production apply는 포함하지 않습니다.
- YouTube.js 18과 current response shape를 채택하고 Official Schedule, Community와 YouTube.js
  관측의 ownership을 collector plane으로 모았습니다. Kakao room catalog의 DB 오류를 not-found나
  빈 관측으로 바꾸지 않으며 retention과 projection 권한을 최소 privilege로 제한합니다.
- Go toolchain과 builder 기준을 `1.26.6`으로, `shared-go`를 `v1.52.3`으로,
  `iris-client-go/v2`를 `v2.1.1`로 갱신하고 일반챗 plaintext/open-chat 판별을 공용
  `kakaoformat` 정본으로 수렴했습니다.
- **service 로그 인코딩이 text에서 JSON으로 바뀝니다.** shared-go 로깅이 text
  인코더를 제거하고 빈 `Format` 기본값이 JSON이 되면서, `hololive-api`,
  `hololive-alarm-worker`, `hololive-youtube-collector`, admin-dashboard backend의
  stdout·파일 로그가 모두 JSON으로 전환됩니다. text 출력을 전제한 로그 수집·grep·
  알림 규칙은 배포 전에 점검해야 합니다. 사람용 ops CLI의 stderr 출력은 종전
  그대로입니다.

### 수정

- collector가 명시적 no-data를 정상 결과로 처리하고 mixed collision batch의 독립 observation과
  checkpoint를 보존하도록 했습니다. missing live tab은 부재 증거로 사용하지 않으며 handoff
  backlog가 재시작 루프를 만들지 않습니다.
- AP/Compose collector의 credential precedence, startup grace, external H3 TLS smoke, readiness와
  rollback 상태 검증을 하나의 fail-closed 계약으로 맞췄습니다. 원격 apply shell fragment도
  독립 ShellCheck가 가능한 구조로 정리했습니다.
- Iris가 `409` + `CLIENT_REQUEST_ID_FAILED`로 답한 reply를 더 이상 즉시 terminal로
  종결하지 않고, generation suffix(`:r1`, `:r2`)를 붙인 새 `clientRequestId`로 같은
  payload를 최대 2세대까지 재전송합니다. 이 code는 durable queue handoff 이전 실패,
  즉 KakaoTalk 부수효과가 없다는 Iris 계약이므로 재전송이 안전합니다. 적용 범위는
  reply(text·markdown)·durable outbox dispatch·비-outbox live media
  (`SendImage`/`SendImages`) 전부입니다. 전송 결과가 불명한 재POST는 동일 id를
  유지하고 FAILED 확정 응답에서만 세대를 올리므로, a9104260이 제거했던 `:aN` 방식의
  중복 발화 위험은 재도입되지 않습니다. `OUTCOME_UNKNOWN`/`PAYLOAD_MISMATCH`/
  `ALREADY_EXISTS`와 code 없는 `409`의 동작은 그대로입니다.

## v2.0.46 - 2026-08-01

### 수정

- heartbeat request는 빈 body를 `idle=false`로 허용하되 JSON body는 1,024 bytes 이하의 단일
  object만 수용하고 `null`, unknown field, 복수 JSON 값과 trailing data를 거부하도록
  OpenAPI·generated client·backend 계약을 일치시켰습니다.
- JSON 및 RSS 응답 크기 상한을 실제 초과 byte까지 읽어 판정하여 limit에서 잘린 유효 prefix를
  정상 응답으로 오인하지 않도록 했습니다.
- 로그인 실패 backoff를 request context가 취소되면 즉시 중단되는 timer로 바꾸고, 내부 Holo API
  base URL은 canonical absolute `http`/`https` origin만 허용하도록 제한했습니다.
- 취소된 request와 분리된 5초 cleanup context로 pgx rollback을 수행하여 오류·panic 경로에서
  transaction을 회수하면서 원래 오류와 panic identity를 보존합니다.
- 관리자 대시보드 heartbeat/WebSocket의 stale callback, reconnect timer와 in-flight ownership
  경합을 차단했습니다.

### 문서·운영

- `youtube-producer`의 현행 4-way Active-Active 토폴로지를 Seoul `b`, main `c`, Osaka `a`,
  Osaka2 `d`와 포트 `30005/30015/30025/30035` 기준으로 README·Project Map·운영 문서에
  정렬했습니다. `b`·`c`는 Docker Compose, `a`·`d`는 host-native systemd가 소유합니다.
- heartbeat OpenAPI SSOT, generated client, backend contract 문서, AP rsync manifest와 Go workspace
  import graph를 최종 코드 경로와 동기화했습니다.

### 의존성

- `shared-go v1.39.0`과 `iris-client-go v1.3.0`을 채택해 durable worker·retry 계약과 Iris
  transport·webhook·Karing 계약을 현재 공개 릴리즈에 고정했습니다.

## v2.0.45 - 2026-07-15

### 문서

- 실제 commit history를 기준으로 app의 주요 과거 릴리즈 기준점을 한국어로
  보완했습니다.

### 릴리즈

- 저장소 app `VERSION`과 runtime artifact 버전을 분리해 정의하고 SemVer 검증 절차를
  도입했습니다.

## [2026-07-13-a9f89640]

### 보안

- Hololive 운영 경계, runtime secret, network·database 접근과 deployment verification을
  강화했습니다.
- worker profile strict envelope와 release provenance를 맞추고 PostgreSQL 18 volume 경계와
  public CI·ephemeral DB ownership 검증을 보강했습니다.

## [2026-07-10-41674269]

### 수정

- dispatch 보상, delivery state machine, migration transaction·timeout을 포함한 SQL 경계
  13건을 수정했습니다.
- migration runner가 `BEGIN`/`COMMIT` block을 실제 transaction으로 재생하고 session timeout을
  pinned connection에 적용합니다.

### 성능

- PostgreSQL `max_connections=60`을 명시하고 hot-path `EXPLAIN` snapshot을 release gate에
  추가했습니다.

## [2026-07-06-9a90f1e7]

### 추가

- YouTube live-session metadata 저장과 alarm-worker live catchup의 표시 phase를
  추가했습니다.
- 방송 이력 분류 규칙을 file로 분리하고 bot의 방송 이력 기능을 Go 1.26 기준으로
  재작성했습니다.

### 제거

- 사용하지 않는 YouTube statistics·milestone subsystem을 제거하되 기존 data는
  보존했습니다.

## [2026-06-28-b36fa988]

### 변경

- 사용자 노출 message를 PostgreSQL SSOT로 전환하고 migration `074`~`082`의 audit·repair
  도구를 추가했습니다.
- `hololive-api`의 bot, admin, llm 세 plane readiness를 dependency-aware `503` 계약으로
  바꿨습니다.

### 수정

- background loop panic을 격리하여 한 plane의 panic이 통합 process 전체를 종료하지 않게
  했습니다.
- `schema_migrations` ledger를 도입해 migration 전체 재적용과 data churn을 차단했습니다.

## [2026-06-26-59ae217a]

### 변경 (호환성 변경)

- production topology를 `hololive-api`, `hololive-alarm-worker`,
  `hololive-youtube-producer`의 세 runtime으로 통합했습니다.
- 기존 kakao-bot, admin-api, llm-scheduler를 각각 `hololive-api`의 bot, admin, llm plane으로
  이동하고 retired service alias와 transitional compose path를 제거했습니다.
- build-first cutover, rollback, health gate, AP sync manifest와 three-runtime CI contract를
  함께 적용했습니다.

## [2026-06-24-3d1fe7d6]

### 변경 (호환성 변경)

- alarm dispatch를 Valkey hybrid 경로에서 PostgreSQL outbox 단일 경로로 전환했습니다.
- community·shorts routing과 published-at resolver의 legacy fadeout 경로를 제거했습니다.

## [2026-05-25-62bb826e]

### 변경

- webhook 전용 `QueuedPool`을 분리 주입하여 Iris와 bot worker-pool 계약을 통일했습니다.
- 공용 utility를 독립 `shared-go` module로 정리하고 Iris worker profile fetch를
  적용했습니다.

## [2026-05-15-4d4b2ae4]

### 변경 (호환성 변경)

- alarm delivery를 PostgreSQL-first outbox로 전환하고 notification egress ownership을
  alarm worker로 이동했습니다.
- retired `dispatcher-go` runtime을 제거했습니다.

### 추가

- Karing alarm delivery, YouTube live session fallback과 delivery guardrail을 추가했습니다.

## [2026-05-10-d6af29a4]

### 변경

- Hololive의 Iris transport를 HTTP/3로 전환하고 OpenBao에서 H3 CA를 render하도록 했습니다.

## [2026-03-15-ef29a66f]

### 추가

- admin-dashboard service를 compose와 deployment script에 추가했습니다.

## [2026-03-09-1220b688]

### 추가

- YouTube scraper service와 runtime lifecycle, 별도 Iris runtime role token을
  추가했습니다.

### 변경

- compose runtime env와 logging entry point를 통합하고 Valkey path·HTTP error 계약을
  정규화했습니다.

## [2026-03-04-f79b4a0a]

### 변경 (호환성 변경)

- alarm, scraper, dispatcher runtime을 Rust에서 Go로 이전하고 deployment를 four-service Go
  topology로 전환했습니다.
- retired Rust·admin module을 제거하고 Go service와 shared contract boundary를
  모듈화했습니다.

## [2026-03-01-1da02d2c]

### 추가

- 기존 llm monorepo에서 Hololive source와 build configuration을 독립 repository로
  이전했습니다.
- 초기 bot, admin, stream ingestion, alarm·scraper·dispatcher runtime과 quality gate를
  구성했습니다.

## [2026-02-28-52cd4f9c]

### 추가

- Kubernetes manifest와 운영 문서를 기반으로 hololive-bot repository를
  초기화했습니다.
