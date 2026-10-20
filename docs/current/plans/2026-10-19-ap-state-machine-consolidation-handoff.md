# AP 상태머신 이관 인계: 로컬 통합과 검증 결과

작성: 2026-10-19(1차), 갱신: 2026-10-20(9차: 전체 리뷰, main 병합 `8f5dec487`(#667), migration 328 운영 적용·관측)
원 계획: [2026-10-19-ap-state-machine-consolidation.md](2026-10-19-ap-state-machine-consolidation.md)
보조 자료: [2026-10-19-ap-state-machine-consolidation-notes/](2026-10-19-ap-state-machine-consolidation-notes/) (설계·결정·하위 작업 지시·리뷰 결과)
다음 세션 시작 프롬프트: [notes/next-session-prompt.md](2026-10-19-ap-state-machine-consolidation-notes/next-session-prompt.md)

1차 세션은 0·1·2단계, 4b(봇 plane), 3a·3b·3c-1을 마쳤다. 2차 세션은 3단계를 끝냈고, 4a-1을 구현·독립 리뷰 2회·local-ci 통과 상태로 남겼다. 3차 세션은 스텝 3 적대적 리뷰를 선행하고 4a-1·4a-1n·4a-2·4a-3을 모두 로컬 커밋했다. 추가 실제 DB 검증에서 발견한 cleanup 교착과 조회 창 완전성 결함도 수정·독립 리뷰·회귀·local-ci를 마쳤다. 최신 main `f258dc645` 기반 통합 HEAD `9bf961190`에서 최종 pre-push-gate r2까지 통과했다. 요청된 로컬 구현·통합·검증은 완료됐다.

9차 세션에서 전체 변경을 리뷰한 뒤 `4b5df2481`을 PR #667로 올려 main에 squash 병합했다(`8f5dec487`, 트리 동일). 같은 main revision으로 migration 328과 API를 운영에 적용했다(§4.6). 선행 `9bf961190`과 후속 `4a1a8eabd`의 운영 적용은 §4.2–4.3에 있다. Git tag는 만들지 않았다. 이하 운영 수치는 각 세션의 확인값이며, 새 세션은 실제 revision·ledger·프로세스부터 확인해야 한다.

## 1. 현재 상태

원 단계들의 기준은 `cb105cc97`이다. 통합은 `c66b62995` 이후 현재 `origin/main`인 `f258dc645`(#666 주석 정리·#659 공통 PR 템플릿 반영)를 기준으로 새 worktree에서 수행했다. 작업 시작 시 메인 체크아웃의 사용자 커밋 `167a5e2ed`와 `docs/current/README.md` 변경은 별도 작업으로 취급했고, 이 작업에서는 수정하지 않았다. 세션 중 메인 체크아웃도 `f258dc645`로 이동한 것을 관측했다.

worktree 경로는 모두 `/home/kapu/work/iris-stack/hololive-bot-apsm-<id>`이다.

| 단위 | id | 브랜치 | HEAD | 기반 | 검증 | 상태 |
|---|---|---|---|---|---|---|
| 0단계 5-1 alarm unit 단위 claim | 0-1 | `fix/alarm-dispatch-unit-clock` | `68f7c9205` | `cb105cc97` | local-ci 통과, 리뷰 반영 | 완료 |
| 0단계 5-2 notification 재발급 fence 미스 | 0-2 | `fix/notification-reissue-fence-miss` | `19511a67c` | `cb105cc97` | 같음 | 완료 |
| 0단계 5-4 lease 회수 지연 ≤ Iris 재발송 지평 | 0-4 | `fix/worker-profile-lease-retention` | `84f3a8cb5` | `cb105cc97` | 같음 | 완료 |
| 0단계 5-5 MarkSent는 SENDING만 | 0-5 | `fix/notification-marksent-sending-only` | `b10837659` | `cb105cc97` | 같음 | 완료 |
| 1단계 live 정책 Go reducer 일원화 | 1 | `refactor/live-state-policy-go` | `b869d0e68` | `cb105cc97` | local-ci 통과, 리뷰 반영 | 완료 |
| 2a runtime_state key shadow 비교(325) | 2 | `refactor/live-runtime-state-shadow-keys` | `dd29a5597` | `b869d0e68` | local-ci 통과, 리뷰 반영 | 완료 |
| 2b 트리거·guard 제거, 명시 refresh(328) | 2b | `refactor/live-runtime-state-explicit-refresh` | `33f7b2e3a` | `68fbc97d5` + `9bf961190` | 독립 리뷰·반증·실제 writer/maintenance smoke·전체 local-ci(`2b-refresh-r1`) 통과 | `4b5df2481`로 후속에 병합(§4.5) |
| 3단계 기반: 0단계 4개 병합 + sendfence | 3a | `refactor/send-fence-notification` | `b4591aa54` | 0단계 병합 | 병합 후 build·vet·test | 완료 |
| 3a notification 소비자 sendfence 이전 | 3a | `refactor/send-fence-notification` | `074c8b265` | `b4591aa54` | local-ci 통과, 리뷰 반영 | 완료 |
| 3b alarm dispatch fence 튜플·단일 DB clock | 3b | `refactor/alarm-dispatch-fence-tuple` | `d9c1438d1` | `b4591aa54` | local-ci 통과(재실행), 리뷰 반영 | 완료 |
| 3c-1 youtube 행 fence·5-3 수정(327) | 3c | `refactor/youtube-outbox-fence-convergence` | `36fa889b4` | `b4591aa54` | local-ci 통과, 리뷰 반영 | 완료 |
| 3c-2 youtube 시각을 DB 시계로 통일 | 3c | 같음 | `77f239ca7` | `36fa889b4` | 리뷰 3건 반영 뒤 local-ci 통과(`3c2-r2`) | 완료 |
| 3c-3 alarm-state claim 제거(코드만) | 3c | 같음 | `710ef7e39` | `77f239ca7` | 리뷰 반영, local-ci 통과(`3c3-r2`), 3c-4 조정 뒤 영향 시험 재실행 | 완료 |
| 3c 추가 검증: 보존 정리·stale sweep 교착 | 3c | 같음 | `b50ac6fd3` | `710ef7e39` | 실제 PG 교착·정리 정체 회귀, 독립 리뷰·계획 smoke·전체 local-ci(`3c-cleanup-session-r3`) 통과 | 완료, 4a 병합됨 |
| 3c-4 `authorized_at` 열 삭제(331) | 3c4 | `refactor/youtube-alarm-authorization-drop` | `b1a395751` | `e36cce5e1` + `9bf961190` | 독립 리뷰·반증·열 삭제 뒤 post/handoff 실DB smoke·전체 local-ci(`3c4-refresh-r1`) 통과 | `4a1a8eabd`에 통합·운영 적용 완료 |
| 4a 준비: 3b + 3a 병합 | 4a | `refactor/worker-handoff-reconciler` | `169944c93` | `d9c1438d1` + `074c8b265` | 병합 후 alarm-worker race test 통과 | 완료 |
| 4a-1 alarm-dispatch reconciler(329·337) | 4a | 같음 | `7f81f1118` | `169944c93` | 기존 리뷰 2회·local-ci(`4a1-r4`) 재사용, 입력 변경 없음 확인 후 hooks 포함 커밋 | 완료 |
| 4a-1n notification reconciler(330·338) | 4a | 같음 | `2c7207703` | `7f81f1118` | 독립 리뷰·delta 반영, 실DB 회귀·smoke, 전체 local-ci(`4a1n-session-r4`) 통과 | 완료 |
| 4a-2 준비: 3c-3까지 병합 | 4a | 같음 | `17d2e504f` | `2c7207703` + `710ef7e39` | golden 재생성, youtube·workerapp·tracking race, manifest·SQL ownership 통과 | 완료 |
| 4a-2 youtube handoff 어댑터(336·339) | 4a | 같음 | `7b4187aa9` | `17d2e504f` | 구현·독립 리뷰 반영, 집계·잠금 회귀·실SDK smoke, 전체 local-ci(`4a2-session-r3`) 통과 | 완료 |
| 조회 창 완전성 보완 | 4a | 같음 | `6be41c653` | `7b4187aa9` | 독립 리뷰·delta, 4개 실DB 회귀·SDK smoke·전체 local-ci(`4a-group-window-r1`) 통과 | 완료 |
| cleanup·handoff 보완 통합 | 4a | 같음 | `698ab8db6` | `6be41c653` + `b50ac6fd3` | youtube egress 전체 race·실DB 경합 후 handoff 정산 smoke·SQL ownership 통과 | 완료 |
| 4a-3 인라인 polling·sentinel·임계값 정리 | 4a | 같음 | `c31721be7` | `698ab8db6` | 독립 리뷰 반영·SDK HTTP 6개 smoke·영향 14패키지 race·전체 local-ci(`4a3-session-r3`) 통과 | 완료 |
| 4b-1 봇 handoff reconciler(326·333) | 4b | `refactor/bot-reply-handoff-reconciler` | `8e8b5aa51` | `cb105cc97` | local-ci 통과, 리뷰 반영, stack 검사 통과 | 완료 |
| 4b-2 엄격 CHECK·legacy reclaim 제거(332·334·335) | 4b2 | `refactor/bot-reply-accepted-strict` | `0b842e2fc` | `dcead379a` + `9bf961190` | 독립 리뷰·반증·strict schema SDK/PG smoke·전체 local-ci(`4b2-refresh-r1`) 통과 | `4a1a8eabd`에 통합·운영 적용 완료 |
| 최종 로컬 통합 | integration | `refactor/ap-state-machine-integration` | `9bf961190` | `f258dc645` + 2a + 4b-1 + 4a | 독립 통합 리뷰·golden 재생성·manifest·SQL ownership·SDK/PG smoke·pre-push-gate(`integration-pre-push-r2`) 통과 | 완료 |
| 후속 로컬 통합(3c-4·4b-2) | followup | `refactor/ap-state-machine-followup` | `4a1a8eabd` | `9bf961190` + `b1a395751` + `0b842e2fc` | reviewer 2명·golden·실제 migrator smoke·full pre-push·integration opt-in·ARM64 배포 검증 통과 | 로컬·운영 적용·관측 완료 |
| 2b 후속 병합(328) | followup | 같음 | `4b5df2481` | `4a1a8eabd` + `33f7b2e3a` | 독립 리뷰·golden·실제 199→200 migrator smoke·repair smoke·FULL pre-push+integration 통과 | main `8f5dec487`(#667) 병합, 328 운영 적용 완료(§4.6) |

CI 로그는 `/tmp/apsm-ci/<id>.log`에 있다. 재실행 로그에는 `-r2`·`-r3`·`-r4`가 붙는다(재부팅하면 사라진다). 2차 세션의 커밋 메시지 원문도 같은 디렉터리의 `*-commitmsg.txt`에 있다.

### 1.1 2b 상태

기존 2b 구현 `68fbc97d5`에 검증된 통합 `9bf961190`을 반영하고 리뷰·실행 검증을 마친 후보 HEAD는 `33f7b2e3a`다. 8차 세션에서 merge 근거를 확인하고 사용자 승인을 받아 후속 `4a1a8eabd`에 `4b5df2481`로 병합했다(§4.5).

- 설계와 다른 점: 영상별 advisory 잠금을 1024 bucket(`hashtextextended(video_id,0) & 1023`)으로 바꿨다. 영상마다 잠그면 3만 key 전체 refresh에서 53200(out of shared memory)이 났다.
- `record_youtube_live_review`는 이제 READ COMMITTED만 받는다. 2a 감사 코드·지표는 2b에서 삭제됐다.

### 1.2 2차 세션에서 바뀐 것

- **3c-2:** 리뷰 3건을 모두 반영했다. (1) outbox 집계 projector의 `sent_at`·`terminal_at`도 DB 시계(`clock_timestamp()`)로 바꿨다. `DeliveryRepository`의 `now` 주입과 `Dependencies.Now`는 지웠다. (2) 재시도 due 시험에 상한을 추가했다. (3) `fanout_fail.sql`이 대상 행을 잠근 뒤 시계를 채취한다. 이 수정의 lock-wait 회귀 시험은 수정 전 실패를 확인했다.
- **3c-3:** 원래 설계는 3c-3의 bulk SQL이 `authorized_at = NULL`을 계속 쓰는 것이었다. 3c-4 구현 중, 그러면 331 적용과 worker 교체 사이에 3c-3 worker가 깨진다는 사실을 발견했다. 그래서 3c-3 커밋을 고쳐(amend, 이전 SHA `86e63af0d`는 버림) 3c-3의 운영 코드가 `authorized_at`을 전혀 참조하지 않게 했다. 구 워커가 남긴 미발송 ENQUEUED 행은 발송 표시에서 SENT로 수렴한다(`TestMarkAlarmSentBatchConvergesLegacyEnqueuedState`). 이로써 331은 일반 순서(migrate 먼저)로 적용할 수 있다.
- **3c-3 CompleteSent:** 부모 결정은 설계 §4.2 유지다. 정상 경로 트랜잭션에는 post 단위 SENT 검사가 없고, commit 응답 유실 read-back에서만 검사해 atomicity breach(`Indeterminate`)로 분류한다. 계약 문서·CHANGELOG도 이 동작에 맞게 고쳤다.
- **3c-3 기타:** INTEGRATION_TEST로 게이트된 동시 실행 시험의 fixture를 운영 형태(tracking 행 포함)로 고쳤다. 운영 관측 SQL(`hololive_msa_hot_path_observability.sql`)에서 claim 적체 블록을 지웠다.
- **3c-4:** migration 331은 블록 안에서 ACCESS EXCLUSIVE를 잡은 뒤 ENQUEUED를 정규화하고, CHECK를 NOT VALID로 교체하고, 열을 삭제한다(색인도 함께 지워진다). VALIDATE는 블록 밖에서 한다. CONCURRENTLY는 쓰지 않으며 manifest ordinal은 187이다. API 중복 poll 시험은 authorized_at 없는 형태(`TestPersistCommunityShortsDuplicatePollKeepsSinglePostState`)로 남겼다.
- **4a-1:** 상세 상태·결정·리뷰 원문·커밋 메시지 초안은 [notes/review-4a-1-pending.md](2026-10-19-ap-state-machine-consolidation-notes/review-4a-1-pending.md)에 있다.

### 1.3 3차 세션: 스텝 3 선행 적대적 리뷰

- 범위: 3a·공통 sendfence, 3b, 3c-1~3c-4. 독립 리뷰어 3명 후 별도 반증 리뷰어 1명이 후보를 재검토했다. 최초 범위에서는 머지를 막을 정확성 결함을 찾지 못했지만, 후속 실제 DB 동시성 검증에서 3c의 보존 정리·stale sweep 교착을 추가 재현했다(§1.6). 따라서 3단계 무결함으로 확정하지 않는다.
- 확인된 비차단 지적: notification ABA 설명에 poll 내부 `processedIDs` 제외가 빠짐, `member_terminal` 주석이 미고정 단위의 종단 형제를 빠뜨림, 331 재적용 설명이 트랜잭션 rollback과 commit 뒤 재적용을 구분하지 않음. 앞 두 건은 `2c7207703`, 마지막은 다음 릴리스 3c4의 `e36cce5e1`로 커밋했다.
- 다중 worker의 claim·정산 잠금 순서 교착 후보는 현재 단일 인스턴스·직렬 runner 계약 아래 도달하지 않아 현재 결함으로는 반증했다. scale-out 안전성을 검증한 것은 아니다.
- 부모가 실제 PostgreSQL testcontainers에서 `-race -count=1`로 실행한 시나리오: 3a stale-owner 정산·동시 격리·재claim 3개, 3b 동시 unit claim·멤버 due skew 2개, 3c 잠금 대기 뒤 begin·retry·fanout 시계 및 follower due floor·claim lease 5개. 모두 통과했다.
- 3c4에서 `TestYouTubeAlarmStateAuthorizationDropNormalizesLegacyClaims` 및 `TestPersistCommunityShortsDuplicatePollKeepsSinglePostState`를 실행해 331 정규화·재적용·열 삭제 뒤 API 중복 poll 수렴을 확인했다.
- 임시 실DB smoke는 unit 잠금에 막힌 `BeginSendRequest`가 아직 멤버를 잠그지 않았음을 `FOR UPDATE NOWAIT`로 확인했다. 이어 pin → begin → stale-owner MarkSent 거부 → 정상 owner 정산으로 두 멤버가 모두 `sent`가 됐다. smoke 파일은 제거했다.
- 4a-1은 `git status`의 74개 파일이 모두 기존 CI 로그 완료 시각 이전이고 staged 파일이 없음을 확인했다. 기존 `/tmp/apsm-ci/4a1-r4.log`의 최종 `Passed`를 재사용해 지정된 메시지로 커밋했다.

### 1.4 3차 세션: 4a-1n 구현·리뷰·검증

- 구현 1명 → 독립 리뷰 1명 → 수정 1명 → 독립 delta 리뷰 1명. 부모가 형식 정리·실제 golden 재생성·검증을 담당했다. 전체 local-ci는 `/tmp/apsm-ci/4a1n-session-r4.log`에서 최종 `Passed`, exit 0으로 완료됐다. 앞선 실패는 신규 시험 lint 12건, 공유 golangci-lint 실행 잠금 충돌, DB 시험 fixture 2건(FIFO의 요청 고정 누락, migration 시험 `$1`의 varchar/text 추론 충돌)이었다. 검사 우회 없이 모두 해소했다.
- notification의 202 requestId를 SENT와 원자 기록하고, 완료·재예약·재발급·exhausted FAILED·QUARANTINED를 soft claim 토큰·requestId·세대로 fence한다. producer rearm은 exhausted FAILED를 되살리지 않는다.
- 확인 대기 SENT는 같은 방을 보류한다. 재발급은 새 `next_attempt_at`으로 기존 FIFO에 재진입하므로 엄격한 생성 순서를 보장하지 않는다.
- 리뷰에서 첫 확인 시각의 짧은 지평 이탈을 발견했다. 실제 notification 발송에서 지평 1,500ms에 첫 확인 5,000ms가 기록되어 회귀 시험이 실패했다. alarm·notification 정산에 기존 attempt_timeout을 전달해 첫 확인을 `min(5s, attempt_timeout)`으로 수정했고, 실DB 회귀가 통과했다. 4a-2도 이 규약을 따른다.
- FIFO 술어를 330 CHECK와 동등한 `handoff_next_check_at IS NOT NULL`로 바꿨다. 격리 PG의 합성 종단 20,000행·미확인 SENT 1행·PENDING 2행에서 old/new 술어 모두 같은 방을 보류하고 다른 방만 반환했다. 읽기 전용 `EXPLAIN (ANALYZE, BUFFERS, SETTINGS)`에서 전체 스캔이 기존 handoff 부분 index를 포함한 BitmapOr로 바뀌었고, shared hit는 712→10이었다. 운영 성능을 측정한 것은 아니다.
- `-race` notification handoff·alarm Consumer 지평 시험 통과. delta 지적에 따라 취소 조회 뒤 재예약이 아니라 soft claim 토큰이 보존됨을 검사하고, 실제 저장된 Iris requestId를 검증했다. 단순 mock 전달·스케줄러 이름 목록 시험은 복원하지 않았다.
- 대신 임시 전 worker 활성 구성 smoke에서 구성된 notification reconciler를 실행해 실제 DB의 미확인 SENT가 checked·next_check NULL로 닫히는 것을 확인했다. FIFO 계획·전 worker 구성 임시 파일은 모두 제거했다.

### 1.5 3차 세션: 4a-2 구현·리뷰

- 구현 1명·독립 리뷰 1명. 요청을 먼저 잠근 CompleteSent의 admission 원자 기록, 고정 멤버 밖 SENT follower를 포함한 실패 정산, source가 같은 ledger만 삭제, 응답 유실 read-back을 구현했다. 부모가 schema golden을 실제 재생성했다.
- 신규 store handoff·DB shape race 시험 통과. sender 시험의 2값 반환 이관 누락 1곳을 고친 뒤 reconciler handoff 시험도 통과했다. 임시 smoke는 실제 Iris SDK의 loopback HTTP GET `failed` → 세대 1 PENDING → source ledger 삭제 → 같은 본문의 `:r1` 재claim 및 부모 PENDING 집계를 확인했고 파일을 제거했다.
- 리뷰의 집계 실패 경로를 실제 PG trigger로 재현했다. 자식 격리 commit 뒤 부모 집계가 실패하면 부모는 SENT로 남고, 기존 복구 후보는 PENDING 부모만 골라 수렴하지 않는다. `TestYouTubeHandoffProjectionFailureRollsBackSettlement`는 기대 SENT/실제 QUARANTINED로 실패했다.
- 기존 canonical aggregate SQL을 handoff 정산 트랜잭션 안에서 재사용하도록 수정했다. 집계 실패 시 자식·ledger·확인 종료도 롤백되어 soft claim 후 다시 확인하며, 위 회귀가 통과했다. post 단위 tracking·알람 발송 표시는 다른 방의 성공일 수 있어 기존대로 되돌리지 않는다.
- 부모 집계를 같은 트랜잭션으로 옮긴 뒤 보존 정리와의 부모/자식 잠금 역전을 재현했다(부모 대기 중 자식 NOWAIT가 55P03). 요청 → 사전 조회한 부모(id 순) → 그룹 delivery → 원장 순서로 고쳤다. 부모 잠금 대기 중 자식이 잠기지 않는 회귀와, 사전 조회 뒤 새 부모가 생기면 확인을 닫지 않고 rollback해 다음 확인에서 정산하는 회귀가 통과했다.
- 잠긴 행만으로 한도 초과가 확정인데도 원장을 기다리는 경로를 재현했다(기대 `ErrHandoffCheckClosed`, 실제 ledger 대기 timeout). 로컬 조건을 원장 전에 판정하도록 고쳤고 실제 DB 회귀가 통과했다. 유효한 그룹만 원장을 잠가 owner 출처 SENT를 확인한다. 구현 파일을 admission·정산·그룹 판정으로 분리했고 독립 delta 리뷰는 문서 불일치 1건만 남겨 반영했다.
- 최종 영향 범위 youtube egress·store·workerapp `-race` 전체가 통과했다. 최종 코드의 임시 실제 SDK/HTTP·PostgreSQL smoke도 `GET=1, delivery=PENDING, parent=PENDING, generation=1, ledgerRows=0, frozenBodyPreserved=true, nextRequest=r1`로 통과했으며 임시 파일은 제거했다.
- 전체 local-ci r1은 아키텍처·lint(0건)·NilAway·build·전체 race까지 통과한 뒤 shared-go promptguard non-race 컴파일에서 실패했다. 외부 체크아웃의 `segments*.go`·`guard.go`가 실패 직전인 2026-10-20 01:59~02:00 KST에 미커밋 변경된 것을 확인했으며 이 변경은 건드리지 않았다.
- 동시 변경을 격리하기 위해 `/home/kapu/work/iris-stack/shared-go-apsm-validation`을 기존 커밋 `dab01e0dd887b70453daabdeefba52122b9739a5`의 detached worktree로 만들고, 4a·3c의 검증용 로컬 `go.work`만 이 경로를 사용하게 했다. `go.work`는 현재 추적 파일이므로 해당 경로 변경은 커밋에서 제외한다. 이 검증에는 원래 shared-go 체크아웃의 미커밋 promptguard 변경이 포함되지 않는다.
- local-ci r2는 외부 worktree 이름을 하드코딩한 lint 소유권 판정 때문에 shared-go에 hololive import 정렬 규칙 등을 적용해 28건을 보고했다. 상위 workspace 모듈을 이름과 무관하게 제외하도록 기존 판정을 일반화했다. hololive의 lint 소유 범위와 외부 모듈의 vet·staticcheck·build·race·non-race 검사는 유지한다. 같은 수정은 3c의 검증 준비에도 적용했다. 전체 local-ci r3(`/tmp/apsm-ci/4a2-session-r3.log`)는 integration-tag·INTEGRATION_TEST까지 최종 Passed(exit 0)였고, hooks를 켠 로컬 커밋 `7b4187aa9`로 마쳤다.

### 1.6 3차 세션: 3c cleanup 동시성 추가 결함

- `710ef7e39` worktree의 실제 `loadStaleSendingCandidates`가 expired SENDING owner를 잠근 동안 `CleanupTerminalOutboxes`가 오래된 FAILED follower의 부모·자식을 잠그고 owner를 기다리게 했다. 이어 같은 stale 트랜잭션에서 실제 `resolveStaleSendingGroups`를 진행하자 `staleSQLState=none, cleanupSQLState=40P01`이 관측되어 cleanup 배치가 롤백됐다.
- 이 재현은 한도 초과가 아닌 2행 논리 그룹이며 다중 dispatcher를 요구하지 않는다. cleanup과 stale 유지보수 루프가 별도로 실행되므로 단일 runner 전제로 반증할 수 없다. 기존 작업의 최초 도입 커밋까지 확인한 것은 아니며, 검토 대상인 3c 현재 코드의 결함이다.
- 보존 정리는 후보 부모·자식을 쥔 채 경쟁 중인 논리 형제를 기다리지 않되, 잠금 누락·행 한도 때문에 불완전한 그룹을 삭제 가능으로 판단해서도 안 된다. 이 불변식으로 3c worktree에서 별도 구현·독립 리뷰·회귀 검증을 마쳤다.
- 1차 수정은 후보 자식·논리 형제를 `SKIP LOCKED`로 읽되 잠그지 못한 행도 불완전 관측으로 반환해 교착을 피한다. 그러나 전역 조회 한도에 닿으면 모든 후보를 보존하는 문제를 독립 리뷰에서 확인했고, 큰 그룹 15행 + 정상 그룹 2행에서 삭제 기대 2/실제 0으로 재현했다(`TestTransitionCleanupOversizedGroupDoesNotStarveOtherKeys`).
- 키별 `L+1` 창과 포화 표식으로 보정해 과대·잠긴 키의 후보만 보호한다. 4개 실제 DB 경계(경합, 과대 그룹 보존, 다른 키 정리 진행, 자식 없는 후보 삭제)와 store 전체 race가 통과했다. SQL 후보 관계는 키별 순위를 보존해야 하므로 handoff의 전역 후보 조각과 별도이며, 기존 equality join을 유지한다.
- 임시 실제 PREPARE/EXPLAIN smoke는 outbox 20,000행·delivery 100,000행, 편향 별칭 289행 추가, 요청 키 1/100, custom/generic 계획을 실행했다. 정상·편향 8개 시나리오에서 원래 요청 ID가 빠지지 않았고, 편향 키만 포화됐으며 request 관계의 행당 재평가와 temp block은 없었다. 운영 성능 보장은 아니다. 임시 파일은 제거했다.
- local-ci r1의 SELECT wildcard와 r2의 빈 map 반환 lint를 수정했다. 모든 projection 열을 명시하고, 자식이 없는 후보는 조회를 호출하지 않되 정상 삭제한다. r3(`/tmp/apsm-ci/3c-cleanup-session-r3.log`)가 integration까지 Passed(exit 0)였고 `b50ac6fd3`로 커밋했다. 4a에 `698ab8db6`로 병합한 뒤 youtube egress 전체 race와 SQL ownership이 통과했다. 임시 실제 DB smoke는 owner·원장 잠금 경합 중 cleanup이 두 outbox를 보존하고, 잠금 해제 뒤 handoff 정산이 두 delivery·부모를 FAILED로 바꾸고 원장을 삭제하는 것을 검증했다. 임시 파일은 제거했다.

### 1.7 3차 세션: 조회 창 완전성 보완

- cleanup delta 리뷰의 후속 가설을 실제 4a DB에서 검증했다. 다른 키 별칭 12행이 한도 12 창을 채우면 owner가 잠기지 않았는데도 handoff 재발급이 성공했다. 잠긴 멤버 여부를 fence에 추가한 뒤 이 회귀는 통과했다.
- 추가 독립 리뷰에서 별칭 11행 + owner만 창 안에 있고 follower가 빠지는 변형을 지적했다. handoff뿐 아니라 `PrepareClaimed`와 stale quarantine도 불완전한 그룹을 오류 없이 수용하는 것을 실제 DB에서 재현했다.
- 같은 SQL 인자 계산을 `logicalGroupWindowLimit(n) = n × (LogicalGroupLimit + 2)`로 공유하고, 빈 요청 또는 관측 수가 그 한도보다 작을 때만 완전하다고 판정한다. claim 기반 해석·stale은 원장 조회 전 오류, handoff는 모든 멤버의 membership 거부·확인 종료로 처리하며 행·원장·세대는 보존한다. SQL의 기존 조회 한도·순서와 raw loader 계약은 바꾸지 않았다.
- 네 회귀(owner 누락, follower 누락, 준비, stale)와 store 전체 race·lint 0건을 확인했다. 실제 SDK/HTTP·PostgreSQL reconciler smoke도 정상 창은 PENDING·세대 1·원장 삭제, 불완전 창은 SENT·세대 0·원장 보존·확인 종료로 통과했다. 임시 파일은 제거했다. 독립 delta 리뷰에서 중요 결함이 없었고 부모 집합 증가 미스의 우선순위를 문서에 명시했다. 전체 local-ci(`/tmp/apsm-ci/4a-group-window-r1.log`)가 integration까지 Passed(exit 0)였으며 `6be41c653`으로 커밋했다. 이 보완은 handoff뿐 아니라 claim 기반 해석·stale expiry/revive에도 적용되며, 별도 3c 브랜치가 아닌 4a·최종 통합에 포함된다.

### 1.8 3차 세션: 4a-3 admission-only cutover

- 구현 1명·독립 reviewer 1명. sender의 Markdown 인라인 `/reply-status` polling과 `GetReplyStatus` 인터페이스 요구를 제거했다. 텍스트 202는 requestId만, Markdown은 success·queued·requestId를 요구하며 즉시 반환한다. handoff는 세 원장의 reconciler만 확인한다.
- `ErrHandoffFailed`와 호출 분기를 삭제하고 `ErrHandoffOutcomeUnknown`을 별칭 없이 `ErrAdmissionOutcomeUnknown`으로 바꿨다. Kind 4종과 409·transport 분류는 유지했다.
- quarantine의 3×lease 기본값과 clamp를 제거했다. consumer와 worker profile 모두 `quarantine_threshold_ms ≥ lease_ms`를 검증하며 생략·0·음수도 거부한다. fixture는 lease에서 계산하지 않는 명시적 고정값을 사용한다.
- 실제 SDK가 임시 HTTP 서버에 POST한 6개 시나리오에서 텍스트 ID-only·유효 Markdown은 성공, 잘못된 4개 admission은 OutcomeUnknown이었고 상태 GET은 모두 0회였다. 영향 14패키지 race가 통과했다. 임시 smoke 파일은 제거했다.
- 독립 리뷰의 P3 문서 지적(구 cutover runbook의 기본값·clamp 설명)을 반영했다. 경계 시험은 오류 문구가 아닌 허용·거부를 검증한다. local-ci r1/r2의 godot 주석 지적을 수정한 뒤 r3(`/tmp/apsm-ci/4a3-session-r3.log`)가 integration까지 Passed(exit 0)였고 `c31721be7`로 커밋했다.

### 1.9 3차 세션: 최신 main 기반 로컬 통합

- worktree `/home/kapu/work/iris-stack/hololive-bot-apsm-integration`, 브랜치 `refactor/ap-state-machine-integration`, HEAD `9bf961190`.
- 0단계 4개·3a·3b·3c 보완이 4a의 조상이고 1단계가 2a의 조상임을 확인했다. 중복 병합 대신 `f258dc645` 위에 2a `dd29a5597` → 4b-1 `8e8b5aa51` → 완성된 4a `c31721be7`를 병합했다. 병합 커밋은 각각 `d6bbbe551`·`1b6804bf1`·`44cf2a811`이다.
- 충돌은 CHANGELOG 합집합과 manifest ordinal로 해소했다. 실제 PostgreSQL 18.6 재생으로 golden을 재생성했고 manifest·SQL ownership 검사를 통과했다. 독립 통합 reviewer 1명은 단계 결합으로 생긴 결함을 찾지 못했다.
- 통합된 봇 runtime을 실제 SDK·임시 HTTP 서버·PostgreSQL로 실행했다. admission 직후 GET=0, 완료는 handoff_completed·payload 삭제, 결과 불명은 manual_review·payload 보존, failed는 실제 409 FAILED 응답을 거쳐 `:r1`로 재발급한 뒤 완료됐다(POST=3, GET=2). 임시 smoke 파일은 제거했다.
- pre-push-gate r1은 개별 브랜치를 합쳐 생긴 migration 시험의 goconst 3건으로 멈췄다. 재적용 여부를 표시 문자열 대신 반복 순서로 판정하도록 6줄을 바꿨고, dbtest lint 0건·4개 실제 DB shape/reapply 시험 통과 뒤 `9bf961190`으로 커밋했다. 검사 억제나 기준 완화는 없다.
- `RUN_INTEGRATION_TESTS=true ./scripts/ci/pre-push-gate.sh` r2가 최종 Passed였다. 로그: `/tmp/apsm-ci/integration-pre-push-r2.log`.
  - 기본 fast/changed 모드가 8개 workspace package pattern을 선택했다. 정본 GOWORK=off vet·tidy, staticcheck, golangci-lint 0건, NilAway, Go/production build, 전체 race·non-race, integration-tag·INTEGRATION_TEST를 실행했다.
  - govulncheck는 모든 검사 모듈에서 호출 경로 취약점 0건이었다. 루트 외 7개 모듈은 각각 require 의존성에 미호출 advisory 1건을 보고했다(수입 패키지 취약점도 0건). 의존성 전체에 advisory가 없다는 뜻은 아니다.
  - 기존 exec-tree 소유권 fixture의 root-owned 파일 허용 사례는 사용할 참조 파일이 없어 SKIP됐다. 이 제한과 Iris/native 전체 계약 미검증(§3.4)은 gate 통과와 별개로 남는다.
- 최종 통합 worktree의 미커밋 변경은 의도적인 로컬 `go.work` 의존성 pin뿐이며 staged·untracked source 변경은 없다.
- 2b(328)·3c-4(331)·4b-2(332·334·335)는 통합하지 않았다. 이 브랜치들의 운영 조건과 승인 경계는 §4 그대로다.

### 1.10 후속 요청: 2b·3c-4·4b-2 준비

- 사용자 요청 “나머지도 끝까지 진행”에 따라 세 후속 브랜치를 기존 통합 `9bf961190`과 맞췄다. 현재 릴리스 integration에 후속을 합치는 것이 아니라, 각 후속 브랜치로 기준을 역병합했다. 운영 gate와 기존 push·PR 금지는 유지한다.
- 단위별 독립 reviewer 3명과 후보 반증 reviewer 1명이 검토했다. 코드 blocker는 없었으며, 운영 gate 미충족은 릴리스 병합·배포 보류 사유이지 로컬 후보 준비의 결함은 아님을 구분했다.
- 2b는 collector의 영수증 기록 안내를 READ COMMITTED로, 수동 head 복원 절차를 exclusive 잠금·CAS·원본 복원·같은 transaction refresh로 보완했다. 4b-2는 332 재실행의 잘못된 23514 안내를 고치고 재적용 시험의 표시 문자열 분기와 시험만 쓰던 submitting 상수를 정리했다. 반증된 3c-4 주석은 변경하지 않았다.
- 각 manifest는 검증된 1–195를 보존한다. 2b는 196=328, 3c-4는 196=331, 4b-2는 196–198=332·334·335만 추가한다. 세 golden을 실제 PostgreSQL로 재생성했고 manifest·SQL ownership 검사를 통과했다.
- 2b 실제 publisher/consumer smoke: 원본 trigger 0개에서 UPCOMING→LIVE→종료가 반영되고 매 단계 drift는 `[0 0 0]`이었다. 실제 `test-terminal-live-head-repair.sh`도 동기화와 stale/count/new-positive/unreviewed-trigger 거부, 비대상·dispatch 보존을 통과했다.
- 3c-4 실제 store smoke: authorized_at이 없는 schema에서 두 방을 SENT로 정산하고, 한 방의 failed handoff만 PENDING·세대 1로 되돌리면서 다른 방과 가장 이른 post SENT 증거를 보존했다.
- 4b-2 실제 SDK/HTTP·PG smoke: strict schema 아래 lease 없는 accepted를 기록하고, failed→409 FAILED→`:r1`→handoff_completed로 정산했다(POST=3, GET=2). 세 임시 smoke 파일은 제거했다.
- 세 후속 후보 모두 독립 리뷰·반증·실제 경로 smoke·전체 local-ci·hooks 포함 로컬 커밋까지 완료했다. 2b `33f7b2e3a`(`/tmp/apsm-ci/2b-refresh-r1.log`), 3c-4 `b1a395751`(`/tmp/apsm-ci/3c4-refresh-r1.log`), 4b-2 `0b842e2fc`(`/tmp/apsm-ci/4b2-refresh-r1.log`)이며 모든 CI가 integration까지 Passed였다. 모든 로컬 `go.work` pin은 커밋에서 제외했다.
- **남은 실행:** 선행 `9bf961190`과 후속 `4a1a8eabd`의 중앙 적용·검증은 완료했다(§4.2–4.3). 8차 세션에서 `video_list` key 0의 원인을 확정하고 2b를 `4b5df2481`로 로컬 병합했다(§4.5). 남은 것은 별도 승인이 필요한 328 운영 적용이다.

### 1.11 선행 운영 적용 뒤 후속 통합

- `hololive-bot-apsm-followup`을 새로 만들고 3c-4 `b1a395751`을 fast-forward한 뒤 4b-2 `0b842e2fc`를 병합했다. 검증 뒤 hooks를 거쳐 **`4a1a8eabd9f500816c59dc66316fe682b4841171`**로 로컬 커밋했다. 선행 integration/release의 `9bf961190`은 보존하고, 현재 배포 source는 `hololive-bot-apsm-followup-release`의 `4a1a8eabd`이다. 다른 작업의 `hololive-bot-retire-yt-outbox` worktree는 건드리지 않는다.
- CHANGELOG의 두 항목을 보존하고 manifest 1–195 뒤에 196=331, 197=332, 198=334, 199=335를 배치했다. 328은 제외했다. 실제 PostgreSQL로 golden을 재생성하고 manifest·SQL ownership을 통과했다.
- reviewer 2명이 YouTube와 bot 관점에서 합성 결과를 독립 검토했고 수정할 결함은 없었다. 9bf961190과 새 schema의 공존, 부분 적용·재적용, strict accepted writer, 인덱스 교체, rollback 하한을 확인했다.
- 격리된 `--network=none` PostgreSQL 18.6에서 **실제 baseline·후속 db-migrate binary**를 실행했다. 195개 baseline 뒤 legacy ENQUEUED와 leased/unleased accepted를 넣자 331은 DETECTED/SENT로 정규화되고 332는 SQLSTATE 55000으로 멈췄다(ledger 196). 테스트의 legacy accepted를 manual_review로 바꾼 뒤 3개가 적용돼 ledger 199가 됐고, 재적용은 0개였다. 23514 거부·정상 unleased accepted 보존·새 index valid·구 index 제거를 확인했으며, 이전 runner는 manifest 밖 ledger 때문에 거절했다. 임시 container·binary는 제거했다.
- `FULL_PRE_PUSH=true` 전체 gate가 통과했다(`/tmp/apsm-ci/followup-pre-push-r1.log`, 816.32s). 8개 workspace 범위·GOWORK=off vet/tidy·staticcheck·golangci·NilAway·build·race/non-race·YouTube.js 372개·govulncheck를 포함한다. 기본값으로 생략된 integration은 `RUN_INTEGRATION_TESTS=true ./scripts/ci/local-ci.sh --integration-tests-only`로 별도 통과했다(`/tmp/apsm-ci/followup-integration-r1.log`, 89.71s). 호출 취약점 0이며, 비root 7개 module에서 미호출 require advisory 1개씩은 남는다. `go.work` local pin은 unstaged·미커밋이다.
- 깨끗한 detached `hololive-bot-apsm-followup-release`에서 VERSION 11.0.0·full revision을 고정한 API·worker ARM64 image를 build하고 SBOM/provenance·신원을 검증했다. API image는 `sha256:a6d3fb93d87a50bba4bacd5de7e3f3d580267897489cd64bb0708276b3e745fc`, worker는 `sha256:c6c6f4abbbabc4aded8e05ca2813b7d0ad106aef2e63c519c3c3b502bee452c1`이다.
- 한정 전송 묶음 `/tmp/apsm-4a1a8eabd-deploy.tar.gz`는 222개 파일, SHA256 `b7c277269e51a31dfffe2bd6a07ae12dd23f09e1be4c2479ba2ed743b9cb88e4`이다. 별도 승인 뒤 전송·설치하고 remote image 신원과 222개 파일의 바이트·소유권·모드를 확인했다.
- 후속 승인 준비용 읽기 전용 집계: YouTube alarm state 962행, `authorized_at` 값 314개, 미발송 ENQUEUED 23행, 발송 ENQUEUED 0행, bot accepted/leased accepted 0/0이었다. 331은 선점시각을 지우고 23행을 DETECTED로 정규화한다. 적용 후 108ecb2e6으로 복귀할 수 없으며 schema와 호환되는 9bf961190 image/profile만 복구점이 된다. 새 DB 백업·secret 백업은 승인되지 않았고 기존 중단 상태를 유지한다. Fallback delta는 없다.

## 2. 사용자 결정

전문: [notes/decisions.md](2026-10-19-ap-state-machine-consolidation-notes/decisions.md)

- **notification(3단계) = (i).** `notification_delivery_outbox` 테이블·API producer·rearm·identity·room FIFO를 유지한다. worker 소비자만 sendfence로 옮겼다(3a). 4단계 reconciler는 notification 원장용 어댑터를 따로 둔다(handoff 확인 열, migration 330·338).
- **2단계 = F1 + READ COMMITTED.** 트리거를 제거하고, Go가 변경 key를 모아 `refresh_youtube_live_runtime_state(text[])`를 트랜잭션 끝에 한 번 호출한다.
- **youtube claim.** `alarm_states.authorized_at` claim을 제거한다(3c-3 완료). 결과 불명 뒤 같은 post의 다른 방이 최대 2분 지연되던 동작은 사라진다. 열 삭제는 다음 릴리스(3c-4, 브랜치 완료)에서 한다.
- **4단계 워커.**
  - Iris `failed`를 보면 재발급한다(generation < 2이고 예산이 남으면 retry + generation+1, `:r1/:r2`).
  - sendoutcome Kind 4종은 유지한다.
  - 지평은 기존 `executor.attempt_timeout`이고, ladder는 5s → 15s → 60s → 5min이며 지평에서 자른다.
- **4단계 봇.** failed는 재발급한다. 상태 어휘 10종을 유지하고, CHECK는 완화 → 엄격 두 단계로 바꾼다.
- 원 계획의 전제 "B안 = 현재 동작"은 사실이 아니었다. 사용자는 이를 알고 다시 결정했다.

### 2.1 2차 세션 부모 결정(사용자 확인 대상 아님, 근거 기록)

- 3c-3 운영 코드가 `authorized_at`을 참조하지 않게 했다(§1.2). 대안은 worker 정지 뒤 migrate하는 것이었지만 무중단 일반 순서를 택했다.
- 3c-3 CompleteSent의 post 단위 검사는 read-back에서만 한다(설계 §4.2). tracking 행과 알람 상태 행이 모두 없는 community/shorts owner는 post 단위 표시 없이 SENT로 commit된다. 운영 생산자(canonicalwrite)는 tracking 행을 함께 만든다.
- 4a-1 텍스트 lane 202는 requestId만 요구한다(bot plane과 같은 규칙). 근거는 SDK fixture의 `delivery:"async"`다.

### 2.2 migration 번호 배정

| 번호 | 단위 | 상태 |
|---|---|---|
| 325 | 2a trigger key audit | 통합 manifest 186 |
| 326 / 333 | 4b-1 완화 CHECK·열 / handoff due index(CONCURRENTLY) | 통합 manifest 187 / 188 |
| 327 | 3c-1 youtube `locked_by` | 통합 manifest 193 |
| 328 | 2b 명시 refresh·advisory 잠금 | 2b 브랜치 |
| 329 / 337 | 4a-1 alarm send unit handoff 열·CHECK / due index | 통합 manifest 189 / 190 |
| 330 / 338 | 4a-1n notification handoff 열 / due index | 통합 manifest 191 / 192 |
| 331 | 3c-4 `authorized_at` drop·`ENQUEUED` 정리 | 3c4 브랜치(manifest 187) |
| 332 / 334 / 335 | 4b-2 엄격 CHECK / submitting lease index / legacy index drop | 4b2 브랜치 |
| 336 / 339 | 4a-2 youtube send_request handoff 열 / due index | 통합 manifest 194 / 195 |

번호가 모두 찼다. 더 필요하면 340부터 쓴다. 통합 manifest는 기존 ordinal 1–185를 보존하고, 병합 순서에 따라 186–195를 `325, 326, 333, 329, 337, 330, 338, 327, 336, 339`로 다시 맞췄다. 각 index는 해당 열 뒤, 336은 327 뒤에 적용된다.

## 3. 완료 범위와 통합 검증

### 3.1 4a-1 커밋 완료 (worktree 4a)

`7f81f1118`로 완료했다. 기존 local-ci 입력을 유지했고, 범위를 확인한 74개 파일만 staging한 뒤 `IRIS_STACK_ALLOW_BULK_STAGE=1`로 hooks를 실행했다. 커밋 메시지는 [notes/review-4a-1-pending.md](2026-10-19-ap-state-machine-consolidation-notes/review-4a-1-pending.md)의 초안 그대로다.

### 3.2 4a-1n: notification 어댑터 (330·338)

하위 작업 지시 [notes/task-4a-1n-notification.md](2026-10-19-ap-state-machine-consolidation-notes/task-4a-1n-notification.md)에 `7f81f1118`을 넣어 구현했고, 독립 리뷰·수정·delta 리뷰와 전체 local-ci를 마친 뒤 `2c7207703`으로 커밋했다(§1.4). 결정 사항은 다음과 같다.

- completed → 확인 기록.
- failed → generation < 2이고 예산이 남으면 PENDING + 세대+1, 아니면 `exhausted=true` FAILED(rearm이 되살리지 않게).
- unknown·404·지평 → QUARANTINED.
- room FIFO는 `handoff_next_check_at IS NOT NULL`인 선행 SENT가 닫힐 때까지 보류한다. 재발급 뒤에는 기존 due 순서로 재진입한다.

### 3.3 4a-2: youtube 어댑터 (336·339)

3c-3 `710ef7e39`를 `17d2e504f`로 병합한 뒤 S4 §2.6 youtube 어댑터를 `7b4187aa9`로 커밋했다(§1.5). 지평은 `workers.youtube_delivery.executor.attempt_timeout`이다. SENT follower를 포함한 재발급, owner source 원장 보호, CompleteSent admission·commit 유실 판정, 실패 정산의 집계 원자성과 잠금 순서를 검증했다. 추가 cleanup·조회 창 보완까지 `698ab8db6`로 병합한 뒤 4a-3을 완료했다.

### 3.4 4a-3 완료

인라인 polling·sentinel·quarantine 기본값과 clamp를 제거하고 관계 검증·alarm 계약을 갱신했다. `c31721be7`과 검증 내역은 §1.8을 따른다.

stack retry/reissue/worker 전체 검사는 실행하지 않았다. 스크립트가 메인 체크아웃에 고정돼 있고 스캔 금지 범위인 `Iris/native/**`를 읽기 때문이다. 공개 SDK의 `CLIENT_REQUEST_ID_FAILED`·세대 상한 2와 작업 코드의 변경 여부를 정적으로 확인했지만 Iris 서버 전체 계약 검증으로 간주하지 않는다. `check-stack-db-access-policy.sh`는 실행해 통과했다(공개 Compose의 PostgreSQL 18 volume 정책 범위).

### 3.5 통합 상태

- 최신 main 기반 로컬 통합과 독립 리뷰·manifest 재정렬·golden 재생성·실제 SDK/PG smoke를 마쳤다(§1.9).
- 최종 pre-push-gate까지 통과했다. 요청된 구현·독립 리뷰·보완·로컬 커밋·통합·검증은 완료됐으며, 같은 입력의 검사를 이유 없이 반복하지 않는다.
- 2b·3c-4·4b-2도 최신 통합 기반 별도 후보로 갱신·검증·커밋했다(§1.10). 운영 선행조건이 충족될 때까지 현재 integration에는 포함하지 않는다. push·PR·배포·운영 migration은 수행하지 않았으며 새 승인이 필요하다.
- 인계 문서와 다음 세션 프롬프트는 원래 위치인 메인 체크아웃의 미추적 파일로 유지한다. source 변경은 각 worktree의 로컬 커밋에 있다.

## 4. 배포·merge 게이트

| 대상 | 조건 |
|---|---|
| 2a(325) | 먼저 migrate한 뒤 API를 올린다. 325가 없으면 지표가 항상 0이라 판정에 쓸 수 없다. |
| 2b(328) | **merge는 8차, 운영 적용은 9차에서 완료(§4.5–4.6).** 네 path는 운영 지표로, 자연 입력이 없던 `video_list`는 사용자가 승인한 격리 PG 직접 트리거 검증으로 충족했다. 원래 merge 조건: (1) 운영 관찰 창 안에서 `hololive_youtube_live_runtime_key_audit_trigger_keys_total{path}`가 `live_snapshot`·`schedule_snapshot`·`video_list`·`video_live_check`·`live_end_finalizer` 각각에서 0보다 크게 증가하고, `…_missing_total`은 어느 path에서도 증가하지 않는다. (2) `scripts/maintenance/check-runtime-projections.sh`의 live 결과가 `0\|0\|0`이다. 배포 순서: API 정지 → db-migrate(328) → 새 API 기동 → `live_runtime_state missing=0 extra=0 changed=0` 확인. 이미지만 되돌리는 롤백은 불가능하다(전진 복구만). 수동 영수증은 READ COMMITTED로 실행한다. |
| 3c-1(327) | migrate를 먼저 한다. nullable 열이라 구 binary와 공존할 수 있다. |
| 3c-2 | migration이 없다. 구·신 binary는 각자 자기 전이만 판정한다. |
| 3c-3 | API와 워커를 같은 릴리스로 배포한다(shared API 변경). 이 릴리스 binary는 `authorized_at`을 참조하지 않는다. |
| 3c-4(331) | 3c-3의 API·worker가 운영에 반영되고 그 이전 image가 0개일 때, 별도 승인을 받아 일반 순서(migrate → 새 image)로 적용한다. 적용 뒤에는 claim 제거 릴리스 이전으로 되돌릴 수 없다. 절차는 `docs/current/runbooks/alarm-worker.md`(3c4 브랜치)에 있다. |
| 4a-1(329·337) | migrate를 먼저 한다(337은 CONCURRENTLY 단일 문장). 텍스트 202 판정이 requestId 필수로 바뀌므로 youtube·notification 텍스트 경로에도 영향이 있다. |
| 4b-1(326·333) | migrate → API. 이전 binary로 롤백하려면 ingress를 멈추고, reconciler가 accepted를 0으로 줄일 때까지 기다린 뒤, 기존 preflight를 통과해야 한다. |
| 4b-2(332·334·335) | 4b-1이 운영에서 돈 뒤, leased accepted 0행을 확인하고 적용한다. |
| 0-4 | 운영 프로필의 `alarm_dispatch lease_ms + recovery_interval_ms`가 144h 미만이어야 한다. 4차 읽기 전용 확인에서 현재 로드된 profile과 파일 hash 일치·관계 충족을 확인했다(§4.1). 배포 직전 candidate profile check는 별도로 수행한다. |
| 4a-3 | `quarantine_threshold_ms ≥ lease_ms`는 현재 로드된 운영 profile에서 충족했다(§4.1). 값은 노출·변경하지 않았다. |

### 4.1 2026-10-20 운영 읽기 전용 확인

- 대상: `hololive-seoul`(`100.100.1.8`), `holo-postgres`의 `hololive` DB. 모든 DB 호출에 `default_transaction_read_only=on`, statement timeout 5s, lock timeout 1s를 적용하고 같은 세션의 `SHOW transaction_read_only = on`을 확인했다.
- API·alarm-worker는 모두 revision `108ecb2e60c538c977965ebea2b54b5409f70435`, 상태 running이었다. API image는 `sha256:106fbf94e45c1b3b62a13695ea390e97c57fe9399d3bc13d2921e9adfc204673`, worker image는 `sha256:d844482b585340d7df6354653a8c72d73fc7bef34d8db2c4237ba8ccc251a7b3`이었다. 실제 변경 직전에는 새로 확인해야 한다.
- migration ledger는 324까지이며 325–339는 하나도 없다. bot accepted와 leased accepted는 모두 0행이었지만, 326·333 선행 릴리스가 아직 운영되지 않아 이것만으로 4b-2 gate가 충족되지는 않는다.
- worker의 비자격증명 profile을 기존 Node로 컨테이너 내부에서 읽어 관계만 계산하고, 기존 healthcheck가 인증한 `/diagnostics/workers`의 로드 hash와 비교했다. diagnostics complete·identity match·loaded bytes match·fileMatch·두 관계 검사는 모두 true였다. profile 원문·설정값·인증값은 출력하거나 파일로 내보내지 않았다. 첫 shell 방식은 exit 127로 실패했다. Dockerfile의 distroless 구성과 포함된 Node를 확인한 뒤 기존 Node 실행 경로로 바로잡았다.
- **확인 시점 판정:** 세 후속 단계 모두 운영 선행 릴리스가 미적용이었다. 이후 사용자가 §4.2의 선행 릴리스 적용 범위를 승인했다. 이 절은 적용 전 읽기 전용 증거이며, 배포 후 상태를 뜻하지 않는다.

### 4.2 선행 릴리스 적용 결과와 후속 gate

- 사용자가 승인 선택 UI에서 **“선행 릴리스 적용·관측 승인”**을 선택했다. 대상은 `9bf961190bdd0a3650894bc30dc015455a1b76f5`의 중앙 `hololive-seoul` API·alarm-worker·DB이다. 로컬 ARM64 build·검증·전송, 선행 migration 10개, 두 서비스의 no-build 교체와 관측을 포함한다.
- 승인 migration 순서는 `325·326·333·329·337·330·338·327·336·339`이다. 후속 `328·331·332·334·335`, AP·인프라·secret 값 변경, push·PR·Git tag publication은 제외한다. 이전 API로 되돌리기는 accepted 배수와 기존 rollback preflight 통과 뒤에만 허용한다.
- 기존 image/profile pair와 배포 파일 복구점을 보존한다. DDL 잠금·재기동·요청/알림 지연 가능성을 안내한 뒤 승인받았다. DB의 암묵적 역마이그레이션이나 secret 원본 되돌리기는 하지 않는다.
- 깨끗한 detached release worktree `hololive-bot-apsm-release`에서 API와 worker를 `linux/arm64`, VERSION `11.0.0`, 정확한 revision으로 build했다. SBOM과 `mode=max` provenance 생성도 확인했다. API image는 `sha256:03cbcbdef7a8b7e93d85fd8b8a7a9b6e680b5e2ac8a1b9ab317c1148eb18d7a8`, worker image는 `sha256:61d2807bc0da0bc4b8a7801429e3c906fd414ecab970bfc80ba95c99db54c7dc`이다.
- 운영 Docker `29.8.2`에서 전송한 두 image의 ID·arm64·full revision을 다시 확인한 뒤 runtime tag를 붙였다. 한정 배포 묶음 218개 파일의 바이트·소유권·모드를 검증했다. 기존 배포 설정·도구의 차이는 주석뿐이었으며 AP·인프라 설정값을 변경하지 않았다.
- 복구점: `/opt/hololive-bot/compose/current/backups/apsm-9bf961190-20261020T004832Z/deploy-tree-before.tar.gz`(SHA256 `3b1a2094b7095b1c3236e72186743fa70c15bd24cf0bbe4bcb81c6ce474345b0`). image/profile pair는 같은 root의 `artifacts/worker-profile/releases/apsm-9bf961190-20261020T004832Z/<hololive-api|hololive-alarm-worker>/{previous,candidate}`에 보존했다. 두 previous pair의 `compose-rollback-service.sh --check`와 두 candidate의 실제 `--check-worker-profile`을 통과했다. candidate profile 바이트는 이전 실행 프로필과 같고 desired source도 일치했다.
- 2026-10-20 `00:53:25Z`부터 migration one-shot을 실행해 **applied=10, skipped=185, total=195, exit 0**을 확인한 뒤 두 서비스만 `up -d --no-build --no-deps --pull never`로 교체했다. API StartedAt은 `00:54:10.895582038Z`, worker는 `00:54:05.101409650Z`이며 관찰 종료에도 healthy·재시작 0·OOM false였다. remote build는 하지 않았다.
- API의 `/health`·`/ready`, LLM `/internal/ready`, admin `/health`, worker `/health`를 실제 검사했다. 두 loaded profile hash·fileMatch·diagnostics complete와 worker의 두 시간 관계도 모두 통과했다. PostgreSQL·Valkey 연결 성공 기록이 있고, 변경 이후 Docker 로그에서 ERROR·panic·permission denied·x509·missing file·OOM 표지는 없었다.
- 비대상 컨테이너 7개의 ID·image·StartedAt은 그대로였다. 중앙 collector는 `youtube-collector-c`, port 30025, healthy이며 실제 healthcheck도 통과했다. secret 값은 변경·출력하지 않았고, manifest mirror 일치와 Hololive metadata 26개 항목의 소유권·모드 일치를 확인했다.
- 운영 host에는 psql이 없어 기존 DB 컨테이너 psql에 `check-runtime-projections.sh`와 동일한 읽기 전용 RR guard 및 두 원본 SQL을 전달했다. 관찰 전후 모두 `on`, bundle `0|0|0|0`, live `0|0|0`이었다. ledger 195개와 승인된 새 migration 10개만 확인했다. accepted/leased accepted는 `0/0`, 이전 API·worker image로 실행 중인 컨테이너는 0개였다(복구용 image 자체는 보존).
- 자연 트래픽 관찰 창은 `00:56:35.950730Z`–`01:06:36.131025Z`였다. 기준값과 추가 샘플 총 10개를 보존해 확인했고, 전체 10개 path의 missing 증가는 모두 0이었다. 추가 관측 thread는 종료·정리했으며 daemon이나 자동 재시도는 남기지 않았다.
- `01:40:00.384671Z`에 같은 API image·StartedAt·재시작 0을 확인하며 추가 조회했다. 최초 기준 이후 약 43분 누적에서도 전체 missing 10개 path 증가는 0이었다. 아래 추가 확인 열은 같은 최초 기준값에서의 증가량이다.

| path | 10분 창 trigger key 증가 | 추가 확인 trigger key 증가 | missing 증가 |
|---|---:|---:|---:|
| live_snapshot | 2348 | 10247 | 0 |
| schedule_snapshot | 22 | 83 | 0 |
| video_list | 0 | 0 | 0 |
| video_live_check | 4 | 38 | 0 |
| live_end_finalizer | 0 | 1 | 0 |

- **후속 판정:** 2b는 `video_list`의 trigger key 증가가 없어 merge·적용하지 않는다. 3c-4·4b-2의 선행 운영 조건은 충족했지만, destructive migration의 별도 적용 승인을 대체하지 않는다.
- **검증 한계:** 첫 metrics body 조회는 healthcheck의 응답 크기 제한으로 실패했다. 서비스 제한을 바꾸지 않고 기존 자격 증명을 호스트 내부에서 인증에만 사용하는 한정 HTTP 조회로 바꿨다. CLIProxy의 구성된 `/v1/models`는 HTTP 200이었지만 반환 목록에서 구성한 기본 모델은 찾지 못했다. alias 해석·실제 생성 가능 여부는 미검증이며 생성 요청은 보내지 않았다.

### 4.3 후속 3c-4·4b-2 운영 적용 결과

- 사용자가 승인 선택 UI에서 **“후속 적용 승인”**을 선택했다. 대상은 `hololive-seoul` API·alarm-worker·`hololive` DB, source는 `4a1a8eabd9f500816c59dc66316fe682b4841171`, migration은 **331·332·334·335**이다. 검증된 image 전송, 기존 9bf961190 image/profile·배포 파일 보존, candidate profile 검사, 네 migration, 두 서비스 no-build 교체와 관측을 승인했다.
- 선점시각 314개 삭제와 미발송 ENQUEUED 23행 정규화, 구 `108ecb2e6`으로 복귀 불가, `9bf961190` image/profile을 복구 하한으로 삼는 점을 설명한 뒤 승인받았다. 새 DB 백업은 만들지 않는다. DB·secret 백업 중단과 기존 보존 자료는 유지하며 임의 역마이그레이션을 하지 않는다.
- 328·AP·인프라·secret 값 변경·push·PR·Git tag publication은 제외했다. 후속 migration은 `2026-10-20T01:59:37Z`부터 실행해 **applied=4, skipped=195, total=199, exit 0**이었다. 두 candidate의 실제 profile 검사와 이전 profile 바이트 일치를 확인한 뒤 두 서비스만 no-build·no-deps·pull never로 교체했다.
- 복구점: `/opt/hololive-bot/compose/current/backups/apsm-4a1a8eabd-20261020T015503Z/deploy-tree-before.tar.gz`(SHA256 `1f6d0c6b19d002c2e67e570e42a950f1457a659ce28e39d400ad4e2293f87d47`). pair는 `/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/apsm-4a1a8eabd-20261020T015503Z/<hololive-api|hololive-alarm-worker>/{previous,candidate}`에 있다. 이전 9bf961190 pair의 rollback `--check`도 통과했다.
- API StartedAt은 `02:10:37.874693148Z`, worker는 `02:10:32.092343320Z`이다. 둘 다 정확한 4a1a8eabd image·loaded profile로 healthy·재시작 0이었다. API·bot ready·LLM ready·admin·worker의 실제 HTTPS 검사, profile fileMatch·complete, worker 시간 관계, PG·Valkey 연결 기록을 확인했고 변경 이후 Docker 로그의 ERROR·panic·권한·x509·missing file·OOM 표지는 0이었다.
- 실제 catalog에서 ledger 199, 328 미적용, authorized_at 열·색인 제거, ENQUEUED 0행, 새 vocab CHECK와 strict accepted CHECK validated, submitting lease index valid, 구 lease index 제거, leased accepted 0행을 확인했다. 읽기 전용 RR 정합성은 bundle `0|0|0|0`, live `0|0|0`이다.
- 중앙 collector-c(port 30025)의 실제 healthcheck, secret manifest/metadata 26개, 설치 파일 metadata 222개가 통과했다. DB 연결 집계는 local 1개와 TLSv1.3 27개이며 tcp_plaintext는 없었다. CLIProxy `/v1/models`는 200이고 생성 호출은 하지 않았다(기본 모델 목록 미노출 한계는 유지).
- **구분해야 할 runtime 변동:** API는 이번 migration보다 앞선 `01:42:22Z`에 동일 9bf961190 image로 이미 재생성돼 ID·StartedAt이 바뀌어 있었다. 01:40까지의 기존 관측만 확정하고 이후 raw counter를 그 기준에서 계산하지 않았다. 승인 후 보존한 previous pair와 migration 직전 snapshot을 기준으로 현재 대상이 동일함을 확인하고 교체했다. 재생성 주체·원인은 확인하지 못했으며, 해당 deunhealth 로그는 재시작 실행이 아닌 새 컨테이너 감시 등록이었다.
- 비대상 7개 container ID·image는 유지됐다. 이 중 PO 컨테이너만 같은 ID/image로 `02:03:17Z`에 StartedAt이 바뀌고 restart count가 3이었으며 현재 healthy이다. 이 작업은 PO 재시작 명령을 실행하지 않았다. 과거 lifecycle event가 남지 않아 그 원인은 미확인이다. 다른 비대상 6개는 StartedAt도 같았다.
- 새 API의 독립 관측 창 `02:15:17.847106Z`–`02:25:18.259162Z`에서 11개 샘플을 확인했다. image·StartedAt·재시작 0이 유지됐고 전체 10개 path의 missing 증가는 0이었다. 종료 뒤 실제 readiness·pair·Docker 로그·drift도 다시 통과했다. 관측 thread는 종료·정리했으며 daemon은 남기지 않았다. 2b 판정과 기준값은 §4.4에 있다.

### 4.4 남은 2b 조건과 재개 순서

- **보류 근거:** 새 관측 창에서도 `video_list`와 `live_end_finalizer`의 trigger key 증가가 0이었다. 이전 9bf961190의 유효 창에서는 finalizer가 증가했으나 video_list는 증가하지 않았다. 서로 다른 프로세스의 관측을 합쳐 5개 경로가 한 창에서 검증됐다고 판단하지 않는다.
- 현재 수집 generation의 `video_list` 대상 22개는 모두 enabled·eligible이며 간격은 300000ms였다. 최근 5000개 관측 metadata 집계에는 video_list 152개가 있었고, 이어 같은 상한으로 payload 모양만 집계했을 때 video_list 154개·잘못된 videos 배열 0개·Premiere 포함 관측 0개였다. 수집 중단을 원인으로 단정할 상황이 아니다.
- `sourceobservation/content_premiere.go:20–27,51–67`은 video_list의 `IsPremiere=true` 사실이 없으면 live session 쓰기와 key 반환 없이 끝난다. 확인한 정규화 payload에는 그 표시가 없었다. **실제 대상 콘텐츠가 없었는지, 수집 범위·목록 선택에서 빠졌는지, 판별·정규화가 누락했는지는 미확인이다.** 이 집계만으로 upstream에 Premiere가 없었다거나 코드 결함이 없다고 확정할 수 없으며, 단순히 기다리면 해결된다고도 판단할 수 없다. 원문 payload·식별자·비밀값을 출력하거나 운영 시험 데이터를 만들지는 않았다.
- 기준 API는 `4a1a8eabd`, StartedAt `2026-10-20T02:10:37.874693148Z`이며 아래 counter 기준 시각은 `02:15:17.847106Z`다. 전체 missing 10개 path의 기준값은 0이다. 원문 샘플은 `/tmp/apsm-ci/2b-runtime-observations.json`에 보존했다.

| path | 기준 trigger counter | 10분 창 증가 | missing 증가 |
|---|---:|---:|---:|
| live_snapshot | 1199 | 2359 | 0 |
| schedule_snapshot | 0 | 8 | 0 |
| video_list | 0 | 0 | 0 |
| video_live_check | 1 | 2 | 0 |
| live_end_finalizer | 0 | 0 | 0 |

다음 세션의 첫 조사:
1. 아래 source 경로와 기존 증거를 먼저 읽고, 운영 조회 전 `hololive-bot-ops`와 DB용 `stack-platform-ops` 지침을 적용한다. 실제 revision·image·StartedAt·ledger를 읽기 전용으로 확인한다. 기존 집계와 새로운 집계를 섞지 않고 같은 프로세스의 counter 기준만 사용한다.
2. 확인된 22개 대상과 해당 관찰 시간대에 실제 Premiere가 있었는지부터 구분한다. 수집 대상 선정·목록/페이지·필터·항목 상한, upstream upcoming 표시와 player publication 근거, Go 정규화, 저장 payload의 `is_premiere`까지 경로를 좁혀 비교한다. raw DTO와 정규화 DTO의 이름 차이를 곧바로 오타나 버그로 취급하지 않는다.
3. 적용 가능한 Premiere 입력을 찾았는데도 key가 0이면 consumer의 적용/skip/실패와 live session 전이·SQL trigger·audit 경계를 추적한다. 입력 자체가 없으면 실제 부재인지 수집/판별에서 누락된 것인지 근거를 남긴다. 원인 후보는 독립 검토로 반증하고, 입증되지 않은 경우 미확인으로 유지한다.
4. 코드 결함이 입증되면 소유 worktree에서 원인을 수정하고 실제 경로의 실패 전/성공 후 검증, 독립 리뷰, 필요한 local-ci를 수행한다. 새 코드나 설정의 운영 반영은 기존 승인에 포함되지 않는다. 인위적인 운영 관측 생성, 수집 범위 변경, gate 완화로 숫자만 채우지 않는다.
5. 현재 gate는 한 유효 창에서 다섯 경로 모두 trigger key 증가 > 0, 모든 path missing 증가 0, live drift `0|0|0`이다. 자연 입력 부재로 이 조건을 충족할 수 없다면, 대체 검증의 증거·보장 범위·한계를 제시하고 acceptance 변경을 사용자에게 요청한다. 로컬 재현을 운영 관측 통과로 바꿔 부르지 않는다.

조사 진입점(검토할 source 기준의 상대 경로이며, 메인 체크아웃을 수정하라는 뜻은 아니다):
- `hololive/hololive-youtube-collector/youtubejs/src/fetch-content.mjs`
- `hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/content_publication.go`
- `hololive/hololive-youtube-collector/internal/runtime/sourceobservation/`
- `hololive/hololive-shared/pkg/contracts/sourceobservation/payloads_v1.go`
- `hololive/hololive-api/internal/youtube/sourceobservation/{content_consumer.go,content_premiere.go,live_runtime_key_audit.go}`
- 같은 API 디렉터리의 `content_consumer_premiere_test.go`, `live_runtime_key_audit_test.go`는 기존 양성 경로의 로컬 검증 근거다.

원인과 gate가 해소된 뒤:
1. gate 통과 또는 명시적인 acceptance 변경 승인 뒤에만 2b 후보 `33f7b2e3a`를 최신 후속 통합에 합친다. 마지막 확인 기준은 `4a1a8eabd`이며 적용된 manifest 1–199를 보존하고 200=328로 둔다. 다른 운영 migration이 생겼다면 실제 ledger와 그 소유 source부터 맞춘다.
2. golden 재생성·독립 리뷰·실제 writer/maintenance smoke·전체 local-ci와 integration·pre-push를 거쳐 로컬 커밋한다. 기존 검사는 관련 입력이 그대로인 범위에서만 재사용한다.
3. 328의 운영 쓰기·API 정지에 대한 별도 승인을 받는다. 승인을 받은 뒤 API 정지 → 328 → 새 API 기동 → drift 0 검증 순서로 진행한다. 328 뒤에는 구 image만으로 되돌릴 수 없으며 전진 복구가 필요하다. 기존 승인은 328·AP/인프라/secret 변경·Git publication을 포함하지 않는다.

### 4.5 8차: video_list 원인 확정과 2b 로컬 통합

1·2단계(원인 확정·gate 해소)는 끝났다. 위 3단계의 328 운영 적용은 아직 승인되지 않았다. 운영 조회는 모두 읽기 전용이었다. DB는 `default_transaction_read_only=on`을 증명한 뒤 집계만 조회했고 식별자·원문 payload는 출력하지 않았다.

**운영 상태(04:13–04:27Z 확인).**
- API·worker는 `4a1a8eabd`이고 StartedAt·재시작 0·OOM false가 §4.3과 같다. ledger는 199개이고 328은 없다.
- collector a/b/c/d는 모두 `video_list` 계약 세대 2(publication 포함)를 발행한다. collector-c revision은 `108ecb2e6`이며, `4a1a8eabd`와의 collector·계약 차이는 시험·주석뿐이다.

**원인(확인된 사실).**
- `video_list`가 live 원본을 바꾸는 경우는 둘뿐이다. 확정 Premiere 사실이 새 세션을 만들 때, 그리고 기존 세션의 `is_premiere` NULL을 true로 바꿀 때다.
  - 이미 true면 REPLAY로 끝나며 쓰지 않는다.
  - 예정 시각·제목이 바뀌어도 이 path의 key는 생기지 않는다(`reconcile/live/premiere.go`, upsert의 `WHERE is_premiere IS NULL`).
- 보존된 `video_list`(10-12 18:43Z~) 전체에서 `UPCOMING_PREMIERE` 항목은 4개 영상뿐이다.
  - 첫 감지는 10-13 09:33Z, 10-14 05:03Z, 10-16 10:03Z, 10-16 11:38Z(예정 시각 3.5~26시간 전)이고, 마지막 항목은 10-17 13:28Z다.
  - 네 영상 모두 `is_premiere=true`·ENDED 세션이다. `video_list`의 live session 적용은 APPLIED 4건(각 첫 감지)과 REPLAY 697건이다.
- 계속 UNRESOLVED인 항목은 오래된 ENDED Premiere 1개뿐이다. 나머지 UNRESOLVED·근거 없음은 cursor 재시작 직후 몇 회에 그쳤다.
- 24h consumer queue는 모두 PROCESSED이고 오류는 0이다.
- 7차의 "payload 154개 Premiere 0"은 좁은 표본이었다.
- 결론: 관찰 창의 `video_list` 0은 자연 입력 부재다. 첫 감지 빈도는 7.4일에 4건이다(소표본 추론으로 약 0.54/일). 10분 창으로는 사실상 충족할 수 없다.
- 독립 reviewer 반증에서 수집·정규화·consumer·trigger 결함 경로는 없었다. `video_live_check` 0건은 세션이 있는 영상만 대상이라 독립 근거로 쓰지 않는다.

**네 path의 운영 충족.**
- 측정 경로: kapu Prometheus(`https://100.100.1.3:9090`, 15초 scrape, 180일 보존)가 API counter를 수집한다.
- 같은 `4a1a8eabd` 프로세스(02:10:37Z 시작)의 04:27:40Z 값:

  | path | trigger_keys |
  |---|---:|
  | live_snapshot | 32422 |
  | schedule_snapshot | 223 |
  | video_live_check | 110 |
  | live_end_finalizer | 2 |
  | video_list | 0 |

- 전 path missing은 0이었다. 04:27:15Z의 RR 읽기 전용 점검은 bundle `0|0|0|0`, live `0|0|0`이었다.

**대체 검증과 승인.**
- 사용자가 "직접 트리거링해서 체크"를 지시했다.
- 운영 DB에 합성 Premiere를 커밋하면 알림 발송·데이터 오염 위험이 있고 운영 쓰기 승인이 필요하다. 그래서 운영 커밋 트리거와 rollback 트리거는 하지 않았다.
- 대신 testcontainers PostgreSQL에서 실제 publisher→consumer로 트리거했다:

  | 시나리오 | 325(`4a1a8eabd`) | 328(`33f7b2e3a`·`4b5df2481`) |
  |---|---|---|
  | 새 세션 | trigger 1·missing 0·extra 0·drift 0 | 수명 모델 행 생성·drift 0 |
  | 반복(REPLAY) | 0·0·0 | drift 0 |
  | live 세션 먼저 → NULL→true | trigger 1·missing 0 | drift 0 |
  | 검토 영수증 있는 NULL→true | trigger 1·missing 0, `reviewed_at` 무효화 | `reviewed_at` 무효화·drift 0 |
  | refresh 없는 같은 변경(음성 대조) | — | drift `0\|0\|1` |

- 사용자는 "대체 인정, 2b 로컬 통합"을 선택했다. 임시 smoke 파일은 삭제했다.

**통합 `4b5df2481c5c5195a9fca1db182fb6e91f0da4f0`.**
- 내용:
  - `4a1a8eabd` + `33f7b2e3a` 병합이다.
  - manifest는 1–199를 보존하고 200=328이다.
  - CHANGELOG는 합집합이고, 328 merge 조건 문장과 runbook·서비스 문서 문단에 `video_list` 대체 검증을 반영했다.
  - golden은 실제 PG로 재생성했으며 자동 병합 결과와 같다.
- 검증:
  - manifest·SQL ownership, build·vet, 영향 패키지 race를 통과했다.
  - 실제 `test-terminal-live-head-repair.sh`가 PASS했다.
  - 실제 migrator 업그레이드(격리 `--network=none` PG 18.6): `4a1a8eabd` binary로 199개(trigger 31개·drift 0) 적용 → 병합 binary로 328만 적용(applied=1, ledger 200, trigger·guard 표 0, live·bundle drift 0). 재실행은 0개였다. 구 binary는 manifest 밖 ledger로 거절했다. refresh 없는 쓰기는 drift `0|0|1`, 명시 refresh 뒤 `0|0|0`이었다.
  - 독립 reviewer: blocker 없음. staging 범위 지적은 반영했다. runbook 문장 순서와 CHANGELOG 325·328 공존 nit는 병합 전 형태라 두었다.
  - `FULL_PRE_PUSH=true RUN_INTEGRATION_TESTS=true ./scripts/ci/pre-push-gate.sh` 통과(734s, exit 0): golangci 0 issues, NilAway, race·non-race, integration-tag·INTEGRATION_TEST, YouTube.js, govulncheck 호출 취약점 0.
  - 로그: `/home/kapu/.cache/tmp/claude-1000/-home-kapu-work-iris-stack-hololive-bot/6d1e7c91-173e-42da-af7e-9e1a7dc84078/scratchpad/2b-merge-pre-push-r1.log`
- hooks를 거쳐 커밋했다. `go.work` local pin은 미커밋이고, `hololive-bot-apsm-followup-release`(`4a1a8eabd`)는 보존했다.

**남은 것(9차에서 해소).** 328 운영 적용은 §4.6에서 끝냈다.

### 4.6 9차: 전체 리뷰, main 병합, 328 운영 적용

사용자 요청: "운영반영전에 이전의 전체작업에 대해 리뷰 잔여 커밋 원격푸쉬후 작업 폴더들 메인 합류 정리 후 라이브반영까지". 이 요청을 리뷰·push·main 병합·worktree 정리·328 운영 적용의 승인으로 보았다. 새 DB 백업은 만들지 않았다(§4.3과 같은 기준).

**리뷰.**
- `code-review high`를 두 범위로 나눠 돌렸다: 후속 4커밋(`9bf961190..4b5df2481`)과 통합 본체(`main...9bf961190`).
- 데이터 손상이나 328 적용을 막을 결함은 없었다. 아래 지적은 수정하지 않고 §5에 남겼다.
  - 후속: 명시 갱신 뒤 상시 drift 감지 없음, shared 잠금으로 늘어난 경합 재시도가 시도 상한을 소모할 가능성, rollback 시험이 refresh 원자성을 검증하지 않음, 328이 SECURITY DEFINER refresh 실행 권한을 runtime 역할에 다시 부여함, 332의 `lock_timeout` 부재·주석 계획 라벨(332는 운영 적용 뒤라 checksum 때문에 수정 불가).
  - 통합 본체: notification 같은 방 후행 행이 handoff 첫 확인(5s)까지 막힘, YouTube 재발급 SQL의 `generation < 2` 하드코딩과 Go 상수 중복, 결정적 정산 오류에 상한 없음, `DLQAfterHandoff` 주석과 claim 키 수집 범위 불일치, 세 reconciler 루프와 bot 판정 코드 중복.
- 운영 `notification_delivery_outbox`는 0행이라 첫 번째 통합 지적의 실제 지연은 관측할 수 없었다.

**publish·병합.**
- 공개 저장소이며 squash 병합만 허용되고, `fast-gate`가 필수다. pre-push hook은 checkout한 branch만, 깨끗한 tree에서만 push를 허용한다.
- followup worktree의 `go.work` local pin과 shared-go 메인 체크아웃의 다른 작업 미커밋 변경을 피하려고, scratchpad에 깨끗한 shared-go(`dab01e0dd`)·iris-client-go(`0d075ea`) export와 새 worktree를 두고 `publish/ap-state-machine-20261020`으로 push했다. pre-push gate가 통과했다(첫 시도는 세션 종료로 race 단계에서 중단, 원격 반영 없음).
- PR #667의 CI 11개가 모두 통과한 뒤 squash 병합했다. main `8f5dec487e5415734ac5bbf92e6788cc5aced13c`의 트리는 검증한 `4b5df2481`과 같다.

**build·전송.**
- 깨끗한 detached worktree `hololive-bot-apsm-328-release`(`8f5dec487`)에서 `kapu-multiarch`로 API만 build했다. alarm-worker·shared·collector 소스는 `4a1a8eabd`와 같아 교체하지 않았다.
- API image `sha256:042f2ee03eee8e7c498d4fd447f00d888da36bf8471896a1a7ac280cf9005b83`, arm64, revision `8f5dec487…`, VERSION 11.0.0. attestation manifest에 SPDX SBOM과 SLSA provenance v1이 있다.
- 배포 묶음 223개 파일(기존 222개 + 328), SHA256 `f8d3cd270f3054a94d68ea186ad6ddc808debefe743895fffe7afe507792ede7`. 운영에서 바뀐 파일은 `manifest.txt`, 추가는 328 하나였고 나머지는 바이트가 같았다.
- 운영 Docker에서 load한 image의 ID·arm64·revision을 다시 확인했다.

**복구점.**
- 배포 파일: `/opt/hololive-bot/compose/current/backups/apsm-8f5dec487-20261020T062226Z/deploy-tree-before.tar.gz`(SHA256 `f0f93e8be923db28a5dcba58dd3f9b95c67045cbec063d0c6a2609f0f39ec232`).
- API pair: `/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/apsm-8f5dec487-20261020T062226Z/hololive-api/{previous,candidate}`. previous는 `4a1a8eabd` image·profile이며 `compose-rollback-service.sh --check`가 통과했다. 328 뒤에는 이 pair로 되돌려도 writer가 refresh를 호출하지 않아 drift가 생기므로 전진 복구만 가능하다.
- candidate의 실제 `--check-worker-profile`·`verify-source`가 통과했고 profile 바이트(`0f68dd4b…`)는 이전과 같다.
- 첫 준비 실행은 `bash -s`로 넘긴 스크립트의 나머지를 rollback check 안의 `docker compose run`이 stdin으로 읽어 candidate 단계 전에 조용히 끝났다. 남은 단계를 원격 파일로 옮겨 `</dev/null`로 실행했다. cutover도 같은 방식으로 실행했다.

**cutover(2026-10-20).**
- 06:23:32.346Z 변경 시작 → 06:23:32.857Z API 정지 → db-migrate **applied=1 skipped=199 total=200**(06:23:35.366Z 완료) → candidate 활성화 → `up -d --no-build --no-deps --pull never hololive-api` → 06:23:46.722Z healthy. API 정지 구간은 약 14초다. remote build는 없었다.
- 새 API: StartedAt `06:23:38.953266689Z`, 정확한 candidate image·loaded profile(`verify-live` 통과), 재시작 0, OOM false.
- catalog: ledger 200(328 applied_at 06:23:35.263Z), 원본 트리거 0, guard 표 없음, `lock_youtube_live_runtime_state_shared()`·`refresh_youtube_live_runtime_state(text[])` 존재, `youtube_live_runtime_state` 216행.
- `/health`·`/ready`·`/internal/ready`·admin `/health`·worker `/health`가 통과했다. 새 API 로그에 PostgreSQL pool·Valkey 연결 성공이 있고 ERROR·WARN·panic·권한·x509·missing file·OOM 표지는 0이다.
- 비대상 컨테이너 9개의 ID·image·StartedAt은 그대로였다(PO의 재시작 3은 §4.3 이전부터 같은 값).

**관측(06:24:36Z–06:34:40Z).**
- 1분 간격 11개 샘플 모두 RR 읽기 전용 bundle `0|0|0|0`, live `0|0|0`, API image·StartedAt 불변, 재시작 0, healthy였다.
- 창 동안 새 API 로그의 ERROR·WARN, `LiveSessionConflictError`, 40P01·40001·55P03, DEAD_LETTER 언급은 모두 0이었다.
- 06:34:58Z 집계: cutover 뒤 관측 queue 1,715건 PROCESSED·2건 PENDING, 최대 attempt 1, 오류 코드·dead letter 0, live head 갱신 112건. live_snapshot·channel_live_check·video_list·video_live_check·schedule_snapshot 등 명시 refresh writer 경로가 실제로 돌았다.
- 관측 스크립트는 종료했고 daemon은 남기지 않았다.

**운영 규모 측정.** cutover 전 읽기 전용으로 328의 전체 재계산 SELECT를 실행하면 10,519 key, 결과 209행, 약 64ms였다. 실제 migration은 컨테이너 기동을 포함해 약 2.5초에 끝났다.

**정리.**
- admin-dashboard: 사용자가 "저장소 제거+worktree 제거"를 선택했다. `chore/retire-admin-dashboard-dir`(`67c5c8577`)이 `admin-dashboard/`(106개 파일 변경)를 지우고 루트 안내·문서 지도·pre-push 경로 조건을 맞춘다. `admin-dashboard-ingress` 서비스는 그대로다. `hololive-bot-remaining-limits` worktree(main에 병합된 `ab68fa10e`)는 제거했다.
- 운영 `/var/tmp/apsm-8f5dec487`의 전송 파일과 복구점은 acceptance 전까지 보존한다. 정리는 별도 승인 대상이다.

## 5. 알려진 위험·미결

- **2b 뒤 병렬성.** 관측 확정이 병렬이 되어 40001/40P01/55P03과 `LiveSessionConflictError`가 실제로 발생한다. 빈도는 측정하지 않았다.
- **2b bucket 잠금.** 같은 bucket의 다른 영상끼리는 commit까지 서로 기다린다. 328 적용 뒤 10분 창에서는 경합 오류가 0건이었다(§4.6).
- **2b 상시 감지 없음.** 325 key 비교 지표를 지웠으므로 writer가 refresh key를 빠뜨리는 회귀는 `check-runtime-projections.sh` 수동 점검으로만 드러난다.
- **9차 리뷰 잔여(미수정).** §4.6의 지적은 후속 작업 후보다. 우선순위가 높은 것은 notification 같은 방 5s 직렬화, 결정적 정산 오류의 무한 재확인, YouTube 재발급 SQL의 세대 상한 하드코딩, 328의 refresh 실행 권한 재부여다.
- **3a ABA 전제.** 같은 workerID의 재claim은 같은 poll 안의 `processedIDs` 제외와 poll 사이 모든 항목 join으로 직렬화한다. 3b의 alarm-dispatch도 단일 인스턴스·직렬 runner에 의존한다. 다중 인스턴스의 claim·회수 UPDATE 잠금 순서는 이번 검토로 보장하지 않는다.
- **3c-3 post 단위 표시.** tracking 행과 알람 상태 행이 모두 없으면 표시 없이 SENT로 commit된다(§2.1).
- **4a-1.** [notes/review-4a-1-pending.md](2026-10-19-ap-state-machine-consolidation-notes/review-4a-1-pending.md)의 "남은 위험"을 본다. 특히 다음 둘이다.
  - sent 조기 노출(R3).
  - 텍스트 duplicate 202에 requestId가 없을 때의 동작(Q6). Iris 확인이 필요하다.
- **4b 관측.** reply outbox attempt `success`는 이제 admission을 뜻한다. handoff 완료를 세는 지표는 없어 대시보드 의미가 바뀌었다.
- **4b failed ⇔ 409 FAILED 동치는 추론이다.**
- **notification 경로의 죽은 코드.** alarm-dispatch의 `delivery_digest` 소비자 경로는 결정 (i)에 따라 그대로 두었다.
- **3c-1 PrepareClaimed.** 한 행이 미스하면 배치 전체를 거절한다(설계 의도).
- **설계 노트의 과거 서술.** `design-S3B`의 3c-4 게이트 서술(3c-3 bulk SQL이 `authorized_at`을 비움)은 §1.2 결정으로 대체됐다. 설계 문서는 고치지 않았다.

## 6. 작업 방식 메모

- **worktree 규칙.** 하위 에이전트의 상대 경로 read/edit는 메인 체크아웃을 가리킨다. 지시할 때 worktree 절대 경로를 반드시 준다. 공통 지시는 [notes/subagent-rules.md](2026-10-19-ap-state-machine-consolidation-notes/subagent-rules.md)에 있다. 노트 파일 안의 `local://apsm-*.md` 참조는 같은 notes 디렉터리의 파일을 가리킨다(`apsm-rules`=subagent-rules, `apsm-design-S4`=design-S4-iris202-reconciler 등).
- **검증 명령.**
  - local-ci: `LOCAL_CI_GO_SCOPE=changed RUN_INTEGRATION_TESTS=true ./scripts/ci/local-ci.sh`(worktree 루트, 10–14분). NilAway 단계는 test 파일의 nil 흐름도 잡는다. 하위 에이전트는 nullable scan 역참조를 자주 놓친다.
  - integration만: `RUN_INTEGRATION_TESTS=true ./scripts/ci/local-ci.sh --integration-tests-only`(임시 docker PG·Valkey). integration-tag 시험까지 부모가 실행한다. 하위 에이전트는 build·test·lint·formatter·Git 쓰기를 하지 않는다.
  - lint: `/home/kapu/go/bin/golangci-lint run -c .golangci.yml ./hololive/<module>/...`.
  - SQL 시계 정책: `python3 scripts/architecture/check-sql-ownership.py`. clock CTE는 인덱스 범위 조건에서 CROSS JOIN 말고 `(SELECT … FROM clock)` 스칼라 하위 질의로 참조한다.
  - 최종 통합 gate: `RUN_INTEGRATION_TESTS=true ./scripts/ci/pre-push-gate.sh`(통합 worktree 루트). 이 명령은 push하지 않는다.
- **검증 의존성 격리.** 다른 작업이 수정 중인 shared-go를 건드리지 않고 `shared-go-apsm-validation` detached worktree를 `dab01e0dd887b70453daabdeefba52122b9739a5`에 만들었다. 3c·4a·integration과 후속 2b·3c4·4b2·followup의 로컬 `go.work`만 이 경로를 사용한다. 이 추적 파일 변경은 의도적으로 staging·commit에서 제외했고 GOWORK=off 정본 검사와 module pin은 유지했다. 해당 worktree와 로컬 pin은 다음 검증에 사용할 수 있도록 보존한다.
- **커밋.**
  - commit-msg hook 형식은 `<type>(<scope>): 한국어 요약`이다. 제목 끝에 `(migration 331)` 같은 괄호를 붙이면 거절된다.
  - pre-commit `staging-scope-guard`를 통과하려면 범위를 확인한 뒤 `IRIS_STACK_ALLOW_BULK_STAGE=1`을 쓴다.
  - 병합 커밋 형식은 `chore(merge): …`이다.
- **리뷰.** 단위마다 독립 reviewer에 [notes/review-prompt.md](2026-10-19-ap-state-machine-consolidation-notes/review-prompt.md)를 주고 돌렸다. blocker·major는 반영했고 minor·nit도 대부분 반영했다. 수정 라운드가 국소적이지 않으면 delta 재리뷰를 한 번 더 돌렸다(4a-1).
- **하위 에이전트 모델.** 2차 세션에서도 기본 `opencodex/gpt-6-astra`가 429였다. 그래서 `task.agentModelOverrides`를 세션 한정으로 `anthropic/claude-opus-5-5:high`로 바꿨다(저장하지 않음). 새 세션에서 429가 나면 같은 override가 필요하다.
- **eval `agent()` schema.** optional 필드가 24개를 넘으면 400(too many optional parameters)이 난다. 중첩 객체까지 모든 필드를 `required`로 둔다.
