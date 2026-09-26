# !라이브 방송 없음 판정 증거 개편

**Decisions:** `DEC-20260926-hololive-live-absence-evidence` (governing), `DEC-20260814-hololive-youtube-three-provider-convergence-v2` (constraint), `DEC-20260829-hololive-live-start-evidence-admission` (constraint), `DEC-20260926-youtube-only-stream-providers` (constraint), `DEC-20260926-hololive-list-reply-fold-default` (constraint), `DEC-20260926-hololive-live-query-read-model` (context)

## Execution capsule

**Goal:** `!라이브`가 확정 방송은 표시하고, 판정 근거가 갖춰진 빈 결과는 "현재 방송 중인 멤버가 없습니다."로 안내하게 한다.
**Context:** 방송 탭·Holodex 목록은 채널 부재를 증명하지 못하고, 보존 종료 증거와 오래된 LIVE가 모든 채널을 확인 불가로 만든다(covered 0/74).
**Constraints:** 명령 경로 원천 호출·표시 전용 상태·보존 pending 삭제·임의 종료 시각 금지. `/live` 결과는 종료 reducer에 넣지 않는다. 운영 배포·migration 적용·데이터 쓰기는 별도 승인이다.
**Evidence:** 이 문서 `근거` 절의 2026-09-26 읽기 전용 DB 조회, YouTube 공개 요청, 코드·테스트 확인.
**Success:** T01~T07 산출물이 AC01~AC07을 만족하고 V01~V04가 통과한다.
**Output:** 관측 계약·migration·helper·collector·consumer·LiveQuery·응답 문구 변경, 계약·서비스 문서 갱신, 로컬 검증 근거와 배포 전 조건.

## 결정 범위

- 채널 확인은 innertube `/live` 주소 해석이다. 요청 채널의 예정 영상(대기 상태 확인) 또는 정상 채널 페이지(`WEB_PAGE_TYPE_CHANNEL`)로 연결되면 공개 실시간 방송의 음성 증거다. LiveQuery만 사용하며 종료 reducer 입력이 아니고, 멤버 한정 방송과 최초공개의 부재를 보장하지 않는다.
- 영상 확인은 player 응답의 사실 필드로 identity, 현재 방송 여부, 시작·종료 시각, 가용성을 판정한다. 신선한 positive가 없는 추적 LIVE는 identity가 맞고 유효한 종료 시각이 있을 때만 해당 영상의 명시적 종료로 canonical에 반영한다(PARTIAL, absence slot 없음).
- D1: 세션과 head가 모두 없는 EXPLICIT_END pending은 채널 차단에서 제외하고 진단으로 남긴다. 경합 시간창은 두지 않는다. head만 있거나 identity가 어긋난 pending은 이 예외가 아니다.
- D2: session=ENDED는 현재 후보와 completeness 차단에서 제외한다(head 부재, 보존 pending, head=LIVE 불일치 포함). 불일치 진단은 유지한다. session=LIVE인데 head가 없으면 예외가 아니다.
- D3: 시작 관측 없이 끝난 상태의 terminal 수용 확장은 범위 밖이다.
- D4: 가용성은 공개, 멤버 한정, 공개 불가, UNKNOWN으로 나눠 확인 시각·근거와 함께 canonical 사실로 기록하고 LiveQuery부터 사용한다. `LOGIN_REQUIRED`나 `ERROR`만으로 공개 불가를 확정하지 않는다.
- D5: 종료 시각이 없는 공개 불가 LIVE는 ENDED로 바꾸지 않고 수명 상태와 가용성을 함께 보존한다. 공개 불가 근거가 만료되거나 재확인에 실패하면 후보 제외를 유지하지 않는다.
- D6: 빈 결과가 확정되면 "현재 방송 중인 멤버가 없습니다."만 안내하고 범위 설명을 붙이지 않는다. 확인이 끝나지 않았으면 "현재 방송 상태를 확인할 수 없습니다."를 유지한다.
- 유지: 활성 등록 Hololive 조회 대상과 멤버 지정 채널 해석, 기존 bot pool과 1초 query 예산, freshness `min(5분, 2×poll interval+30초)`, 명령 경로 원천 무호출, 확정 방송(공개·멤버 한정·최초공개) 표시.
- 범위 밖: 부재 기반 자동 종료 개편, 레거시 head 전량 보강, UPCOMING 전수 정리, Holodex 증거 우선순위 변경, `CONFLICT_KEEP` 점검, pending 쓰기 최적화, session=ENDED/head=LIVE 17건 데이터 복구.

## 근거

2026-09-26 운영 DB는 read-only guard를 확인한 세션에서만 읽었고, YouTube에는 공개 GET과 innertube 요청만 보냈다. 코드·데이터 변경은 없었다.

- 채널 판정: 배포 직후 Hololive 74채널 중 covered 0(`inconsistent` 63, `confirming_end` 7, `incomplete` 3, `stale` 1).
- 방송 탭 첫 페이지는 최대 30개이고 상태순이 아니다. 24시간 COMPLETE 53,280건 중 3,011건에서 LIVE가 종료 방송 뒤에 있었다. 7일간 방송 탭이 놓친 일반 방송은 관측되지 않았다. Holodex만 본 14건은 최초공개 10, 집계 경계 착시 2, 삭제 1, Holodex 오보 1이었다. Holodex는 코보의 실제 방송을 놓쳤다.
- 부재로 종료된 15건 중 12건은 YouTube `endTimestamp`보다 2.5시간~19일 늦게 종료됐고 조기 종료는 0건, 3건은 판정 불가였다.
- 차단 요인: 세션·head 없는 EXPLICIT_END 809행(60채널), head 없는 ENDED 세션의 pending 367행(61채널), session=ENDED/head=LIVE 17행(17채널), 신선한 positive가 없는 LIVE 16행(전부 head와 LIVE 관측 시각 보유). 예정 시각이 지난 UPCOMING 234행은 현재 차단 대상이 아니다.
- pending 보존은 `TestReduceRetainsSupersededEndEvidence`가 고정한 의도된 동작이다. 반복 기록으로 `youtube_live_pending_ends` 갱신 누계는 약 4,695만 건이다.
- 두 원천 모두 `source_event_at`을 보내지 않아(최근 1시간 전부 NULL) 관측 유효 시각이 `scheduled_for`로 계산된다(`EffectiveAt`).
- `/live` 74채널 시험: LIVE 13(수집 결과와 13/13 일치), 예정 연결 38, 채널 페이지 23. 멤버 한정 방송 `maPfvoIK-MU`가 `isLiveNow=true`인 동안 HTML과 innertube `/live` 모두 2027년 예정 대기실을 골랐다.
- 경량 경로: 채널 확인은 요청 1~2회에 압축 0.5~8KB, 영상 확인은 요청 1회에 1.3~6.6KB이다. HTML 경로는 페이지당 0.8~1.6MB였다. 원시 player 응답은 `endTimestamp`와 `channelId`를 포함하지만 현재 helper 파서는 버린다.
- 응답 해석: WEB player는 공개 LIVE와 종료 영상에도 `UNPLAYABLE`을 준다. 멤버 한정 LIVE도 `isLive=true`를 준다. 종료 영상은 `isLive`를 생략하고 `isLiveNow=false`와 `endTimestamp`를 준다. 비공개는 `LOGIN_REQUIRED`와 `messages`, 이용 불가는 `ERROR`였다. 최초공개 표본은 모두 `isLiveContent=false`이면서 `liveBroadcastDetails`가 있었고 일반 업로드 1건은 없었다.
- YouTube Data API `search.list`는 기본 하루 100회 별도 할당이라 상시 채널 확인에 쓸 수 없다.

## 작업

### T01 관측 계약 설계

`docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md`에 youtubejs provider의 두 관측을 추가한다.

- 채널 `/live` 확인: subject는 채널이다. 선택 결과 유형(LIVE 영상, 예정 영상, 채널 페이지, UNKNOWN), 선택 영상 ID, 채널 identity 확인 여부와 UNKNOWN 사유를 담는다. `youtubejs_channel_live` 작업이 기존 `live_snapshot`과 별도 envelope로 발행하며 성공·실패·completeness를 섞지 않는다.
- 영상 상태 확인: subject는 영상이다. 영상·채널 identity, `isLiveNow`, 시작·종료 시각, `isLiveContent`, `liveBroadcastDetails` 존재, 가용성, UNKNOWN 사유를 담는다. 대상은 target projection이 신선한 positive가 없는 canonical LIVE 영상으로 만든다.
- 결과별 재확인 주기와 증거 만료, 확인당 요청 수 상한, 계약 세대·kind 허용 목록·retention·grant 변경 목록을 정한다.

### T02 migration과 canonical 저장소 준비

다음 번호 migration으로 kind 허용 목록, `observation_contract_generations` 행, target kind 제약, 채널 확인 최신값 테이블, 영상 가용성 테이블(확인 시각, observation 근거, 판정 방법), grant, manifest와 schema golden을 준비한다. migration 211의 LIVE coverage 부분 인덱스는 T05 이후 다른 사용처가 없음을 확인한 뒤 제거 migration을 함께 준비한다.

### T03 YouTube.js helper와 collector 변경

- helper: 주소 해석과 player 조회를 RPC로 제공하고 `endTimestamp`, `channelId`, `isLiveNow`를 추출한다. 판정 순서는 identity, 방송 상태와 시각, 모순 검사, 가용성, 해석 불가 UNKNOWN이다.
- Go collector: `youtubejs_channel_live`에 채널 확인 envelope를 추가하고 영상 상태 확인 runner를 새로 둔다. 응답 identity를 요청 subject와 대조한다.
- fixture: 공개 LIVE, 멤버 한정 LIVE와 예정 연결 반례, 예정 연결, 채널 페이지, 종료 방송, 종료 최초공개, 멤버 한정 종료, 비공개, 이용 불가, 로봇 확인 형태(합성), 모순 필드, 구조 불일치.

### T04 consumer와 target projection 변경

- 영상 상태 consumer: 현재 LIVE 사실은 기존 positive 규칙으로, 유효한 종료 시각은 해당 영상의 명시적 종료로 반영한다. 가용성은 가용성 테이블에 기록한다. UNKNOWN과 종료 시각 없는 공개 불가는 수명 상태를 바꾸지 않는다.
- 채널 확인 consumer: 채널별 최신값만 갱신하고 오래된 관측이 최신값을 덮지 않는다. live reducer, pending, absence slot을 만들지 않는다.
- target projection: 신선한 positive가 없는 canonical LIVE 영상을 영상 상태 확인 대상으로 만들고, 종료되면 대상에서 뺀다. 대상 생성 지연을 측정한다.

### T05 LiveQuery 판정 변경

- coverage 원천을 absence slot에서 채널 확인 최신값(신선도 예산 안의 음성 유형)으로 바꾼다.
- D1·D2 대상은 채널 판정 사유와 분리한 비차단 진단으로 `live query incomplete` 로그에 남긴다.
- 신선한 positive가 없는 LIVE는 영상 확인으로 종료됐거나 신선한 공개 불가 근거가 있을 때만 차단에서 빠진다.
- 멤버 지정 조회도 같은 규칙을 쓰고 방송이 없을 때의 `CMD_MEMBER_NOT_LIVE` 문구는 유지한다.
- 단일 snapshot, 1초 예산, bounded read를 유지하고 `EXPLAIN (ANALYZE, BUFFERS)`로 확인한다.

### T06 응답 문구와 문서 갱신

- 빈 결과가 확정되면 "현재 방송 중인 멤버가 없습니다."를 보낸다. `CMD_LIVE_STREAMS`의 Count 0 표준 본문을 migration으로 바꾸고 사용자 지정 본문과 채널 override는 보존한다.
- `docs/current/services/hololive-api.md`, `docs/current/services/youtube-collector.md`, `docs/current/runbooks/youtube-collector.md`, `docs/current/architecture/MESSAGE_STYLE_GUIDE.md`를 갱신한다.

### T07 통합 검증과 배포 전 조건 정리

영향 모듈의 테스트·lint·NilAway와 decision catalog를 실행하고, 배포 순서(API와 migration을 collector fleet보다 먼저), 계약 세대 활성화, 롤백 지점, 운영 비교 관측 항목(멤버 한정 방송, 동시 방송, 로봇 확인 응답, 기존 positive와 `/live` 음성 충돌)을 릴리스 계획 입력으로 남긴다.

## 수용 기준

### AC01 빈 결과 안내

모든 대상 채널에 신선한 음성 채널 확인이 있고, 신선한 확정 LIVE가 없고, 해소되지 않은 LIVE 후보와 필요한 확인의 UNKNOWN·충돌이 없을 때만 "현재 방송 중인 멤버가 없습니다."가 나온다. 하나라도 빠지면 "현재 방송 상태를 확인할 수 없습니다."가 나온다.

### AC02 확정 방송 표시

신선한 확정 LIVE는 공개·멤버 한정·최초공개 모두 채널 확인이 음성이어도 표시된다. 멤버 한정 LIVE가 진행 중이고 `/live`가 예정 영상을 고르는 반례에서 빈 결과 안내가 나오지 않는다.

### AC03 보존 증거와 차단 분리

세션·head 없는 EXPLICIT_END와 session=ENDED(head 부재, head=LIVE 포함)는 채널을 차단하지 않고 진단에 남는다. session=LIVE이면서 head가 없으면 계속 차단한다. pending 행은 삭제되지 않고 보존 테스트는 바뀌지 않는다.

### AC04 영상별 종료 반영

신선한 positive가 없는 추적 LIVE는 identity가 맞고 유효한 종료 시각이 있을 때만 ENDED가 되며 `ended_at`은 그 종료 시각이다. absence slot은 생기지 않는다. 공개 불가, UNKNOWN, 종료 시각 없음은 수명 상태를 바꾸지 않고, 공개 불가 근거가 만료되면 다시 차단 후보가 된다.

### AC05 응답 해석

`UNPLAYABLE`이어도 LIVE 메타데이터가 있으면 LIVE이고, `isLive` 생략과 유효한 종료 시각은 종료 근거다. `LOGIN_REQUIRED`나 `ERROR` 단독은 공개 불가가 아니며, 로봇 확인·구조 해석 실패·identity 불일치·모순 필드는 UNKNOWN이다.

### AC06 채널 확인 격리

채널 확인 결과는 live reducer 입력, pending, absence slot을 만들지 않는다. 채널 페이지 연결은 요청 채널의 정상 채널 응답 구조가 확인될 때만 음성이고, 예정 연결은 요청 채널의 예정 영상과 대기 상태가 확인될 때만 음성이다.

### AC07 요청 비용

채널 확인은 요청 2회 이하, 영상 확인은 요청 1회다. 종료가 canonical에 확정된 영상은 다시 확인하지 않고, 공개 불가·UNKNOWN은 T01의 재확인 주기를 따른다.

## 검증

### V01 계약·helper·collector 테스트

Node helper 테스트와 Go contract·collector 테스트를 `-race`로 실행하고 T03 fixture 범주를 모두 포함한다. 공개 요청 소표본으로 요청 수와 압축 전송량을 다시 측정한다(읽기 전용).

### V02 DB·consumer·LiveQuery 테스트

`hololive-dbtest`로 migration·schema golden, 영상 상태와 채널 확인 consumer, pending 보존, LiveQuery 판정 경계, 1초 예산의 `EXPLAIN (ANALYZE, BUFFERS)`를 검증한다.

### V03 명령 smoke

실제 command, repository, template로 AC01과 AC02의 응답을 확인하고 명령 경로의 upstream 호출이 0회임을 확인한다.

### V04 결합 gate

영향 모듈 lint·NilAway, `bash tools/checks/check-decision-catalog.sh check --submodules`, 영향 패키지 race를 통과한다.

## 중단 조건과 전달

- 사실 필드로 판정할 수 없는 `/live`·player 응답 구조가 나오면, 확인 비용이 AC07을 넘으면, 계약 세대 전환 순서를 지킬 수 없으면 해당 작업을 멈추고 보고한다.
- 운영 배포, migration 적용, collector fleet 반영, 운영 비교 관측은 별도 승인과 릴리스 계획으로 진행한다.
