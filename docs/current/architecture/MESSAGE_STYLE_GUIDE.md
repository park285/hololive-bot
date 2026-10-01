# Message Style Guide

KakaoTalk 사용자 노출 문구(텍스트 메시지·알림 푸시·에러/안내 단문)의 단일 스타일 규범.
2026-07 미니멀 정돈 개편(마이그레이션 084~087)의 근거 문서이며, 이후 모든 신규/수정 문구는 이 가이드를 따른다.

## 1. 범위와 SSOT

- 문구의 SSOT는 DB 카탈로그 2개다: `notification_templates`(Go text/template 본문, template_key), `message_strings`(namespace/key/value). 코드 인라인 문구는 허용하지 않으며, 남은 인라인 라벨은 `timefmt` 네임스페이스로 추출한다.
- `message_strings`는 bot plane·llm plane·alarm-worker가 기동 때 한 번 적재하고, 코드가 쓰는 타입 있는 key(`messagestrings.Key`)와 동적 조회 namespace를 검증한다. 누락되면 기동에 실패하며, 코드 대체 문구·조회 중 재적재·`FallbackSentinel`은 없다. 템플릿 렌더 실패 응답은 `error/command_processing_failed` 문구를 쓰고, 예약 알림은 렌더에 실패하면 발송하지 않는다(`DEC-20260926-hololive-message-strings-startup-validation`). 새 key를 추가하면 `messagestrings` key 목록과 시드 SQL 계약 테스트(format 인자 수 포함)를 함께 갱신한다.
- 소비 plane은 4곳: bot plane formatter(`hololive-api/internal/planes/bot/.../formatter/`), llm plane scheduler(`hololive-api/internal/planes/llm/runtime/formatter_llm_scheduler.go`), alarm-worker(`hololive-alarm-worker/internal/app/workerapp/`), shared youtube outbox(`hololive-shared/pkg/service/youtube/outbox/`). 같은 키를 복수 plane이 렌더하므로 문구 변경 전에 §11 소비자 매트릭스를 확인한다.

## 2. 톤 원칙

- 합쇼체(`-습니다`) 고정. 반말·해요체 금지.
- 감탄 억제: 느낌표는 축하 표면(생일·주년·마일스톤·방송 시작)에서 문장당 최대 1회. 그 외 전부 마침표.
- 사과·수사 금지: "죄송합니다", "축하합니다! 🎊" 같은 중복 감정 표현을 쓰지 않는다.
- 문장 종결에는 마침표를 찍는다. 헤더 행·라벨 행·목록 항목 행은 마침표 없음.
- 정보 밀도 우선: 같은 정보를 더 짧게 쓸 수 있으면 짧은 쪽을 쓴다. 수식어보다 데이터(시각·수치·링크)를 앞세운다.

## 3. 이모지 — 역할 고정 화이트리스트

아래 14개만 사용한다. 글리프는 장식이 아니라 역할 표지다.

| 글리프 | 역할 | 허용 위치 |
|---|---|---|
| 🔴 | 라이브(진행 중) | 라이브 목록 헤더, 진행 중 항목 행, 방송 시작 푸시 헤더 |
| ⏰ | 시간·예정 | 시간 정보 행, 방송 예정 푸시 헤더 |
| 📅 | 일정·달력 | 예정 목록·채널 일정·기념일 달력 헤더 |
| 👤 | 멤버 | 멤버 목록·프로필 헤더 |
| 📰 | 뉴스 | 뉴스 다이제스트 헤더 |
| 📊 | 통계 | 구독자·순위 헤더 |
| 🔔 | 알림·알람 | 알람 목록 헤더, 일반 알림 푸시 헤더, 상태 ON |
| 🔕 | 알림 꺼짐 | 상태 OFF |
| 🎂 | 생일 | 생일 항목 행·축하 메시지 |
| 🎉 | 주년·달성 | 주년/마일스톤 항목 행·축하 메시지 |
| ✅ | 완료 확인 | 설정/해제 완료 단문 접두 |
| ℹ️ | 안내 | "이미 그런 상태"·중립 안내 단문 접두 |
| ⚠️ | 경고 | 졸업 멤버 등 경고 단문 접두 (error 계약) |
| ❌ | 오류 | error 네임스페이스 접두 (계약 — 변경 금지) |

배치 규칙:
- 한 행에 글리프 최대 1개.
- 허용 위치는 목록형 메시지의 1행 헤더, 상태/안내/오류 단문의 접두, 시맨틱 행(🔴⏰🎂🎉)뿐이다.
- 상세 행·URL 행·섹션 브래킷에는 글리프 금지.
- 그 외 글리프는 전면 금지: 🌸 📺 🎬 🔗 👥 💡 📌 📘 🗣️ ✨ 📋 🌐 📱 📝 🗞️ 💬 🎊 📍 등 전부.

## 4. 구조 패턴

- 목록형 메시지 1행 헤더: `<글리프> <제목> · <N>개`(단위는 `개`·`건`·`명` 중 대상에 맞게). 기간·표시 한도 같은 헤더 보조 행은 헤더 바로 아래에 두고, 머리 문단 끝에는 빈 줄 1개를 둔다.
- 메시지 내부 섹션 구분: `[섹션명]` 브래킷, 글리프 없음. (예: 도움말 명령 그룹, 멤버 목록 기수 그룹)
- 번호 목록: `N · ` 마커. 카카오 일반채팅이 `1. ` Markdown 목록의 빈 줄을 합치므로 쓰지 않는다. 상세 행은 들여쓰지 않는다.
- URL은 단독 행에 bare로 둔다. 접두 글리프·라벨 금지. 항목의 마지막 행에 배치.
- 여러 줄 항목의 경계는 `──────────` 한 줄이며 앞뒤에 빈 줄을 두지 않는다. 첫 항목 앞과 마지막 항목 뒤에는 넣지 않는다. 한 줄 항목끼리는 구분선 없이 이어 쓴다(예: 알람 목록). `━━━`·`---` 등 다른 구분선은 금지.
- 빈 결과는 한 줄: `<글리프> <대상>이(가) 없습니다.` (+ 필요 시 사용 안내 1줄).
- 사용 안내는 글리프 없이 `예) {{.Prefix}}알람 추가 페코라` 형식, 또는 명령 나열. 안내 예시 멤버명은 `페코라`로 고정.

## 5. org 라벨 정책 — unmarked default

- Hololive 소속은 무표기(브래킷 억제). 그 외 org만 `[니지산지] 이름` 형식으로 표기한다.
- 라벨 값의 SSOT는 `message_strings`의 `org` 네임스페이스. Hololive 억제는 `constants.OrgHololive` 비교 단일 헬퍼(도메인 규칙)로 구현하고, org 미등록 값은 raw passthrough(`[VSPO]` 등)한다.
- alarm 경로의 Go 하드코딩 org 맵(`formatter_alarm.go`)은 이 헬퍼로 대체한다. streams 경로의 `[Holo]` 표기(및 해당 테스트 기대치)는 정책 적용 커밋에서 함께 제거한다.

## 6. 명령어 표기 canon

파서가 별칭을 여럿 수용하더라도, 사용자 노출 문구에는 canonical 형태 하나만 쓴다.

| 기능 | canonical | 파서 수용 별칭(참고) |
|---|---|---|
| 도움말 | `{{.Prefix}}도움말` | 도움말·도움·help·명령어·commands |
| 행사 알림 | `{{.Prefix}}행사 켜기 / 끄기 / 상태` | 행사·행사알림·이벤트·이벤트알림 × 켜기/on/구독 등 |
| 뉴스 알림 | `{{.Prefix}}뉴스알림 켜기 / 끄기 / 상태` | — |
| 알람 | `{{.Prefix}}알람 추가/제거/목록/초기화 [멤버명]` | — |

## 7. 시간·숫자 표기

- 절대 시각: `01/02 15:04` (KST, 24시간제). 요일·연도는 표면이 요구할 때만.
- 상대 시간: `(N분 후)` / `(N시간 M분 후)` / `(N일 후)`. 미정: `시간 미정`.
- 시간 조각 문자열은 `message_strings`의 `timefmt` 네임스페이스가 SSOT. 절대시각+상대 복합형(streams, `stream_time_*`)만 필수 key다. bare 상대형 `relative_*`는 알람 다음 방송 표시(migration 233에서 제거)만 썼으므로 **미사용 시드 잔존**이다.
- 큰 수는 기존 `formatNumberKR` 파이프 유지.

## 8. '전체보기' 접기(fold) 정책

- `internal/templateview.FoldForSeeMore(text)`는 글자 수 임계 없이 머리 문단(첫 빈 줄 앞, 최대 4줄) 끝에 ZWSP 500개를 붙입니다. 4줄 안에 빈 줄이 없으면 첫 줄만 남깁니다. 빈 본문·한 줄 본문·이미 패딩된 본문은 그대로 유지합니다.
- `BOT_SEE_MORE_FOLD`는 bot·llm plane의 기능별 접기 스위치입니다. 기본 `true`이며 `false`이면 프로그램이 자동 패딩을 넣지 않습니다. 카카오톡 자체의 긴 메시지 접기는 제어하지 않습니다.
- 라이브·예정 방송·채널 일정·방송 이력·알람 목록·멤버 목록·캘린더 텍스트는 표시 결과 0건이면 안내문, 1건이면 펼침, 2건 이상이면 접습니다. 멤버 지정 조회에도 동일하게 적용합니다. 멤버 목록은 정규화 후 유효 멤버 수, 캘린더는 nil 멤버를 제외한 표시 건수를 셉니다. 표시 한도·필터 안내는 별도 항목으로 세지 않습니다.
- 뉴스 직접 조회와 예약 뉴스는 공통 `templateview.BuildMemberNewsDigest`를 사용합니다. 표시 블록은 `TopItems` 수와 `MoreSummary`가 있으면 한 블록을 합친 값이며, 1블록은 펼침·2블록 이상은 접습니다. `TotalCount`는 접기 기준이 아닙니다. 정상 빈 뉴스는 템플릿 안내문을 펼쳐 보냅니다.
- 주간·월간 행사 요약은 LLM formatter가 소유합니다. 원본 행사 1건은 펼침·2건 이상은 접으며, 빈 행사는 기존 미발송 계약을 유지합니다. LLM 요약이 있으면 행사 목록을 함께 반복하지 않습니다. 예약 뉴스·행사의 렌더 오류와 빈/공백 본문은 enqueue하지 않습니다.
- 프로필과 전체 도움말 텍스트는 유효 본문이 있으면 길이와 무관하게 접습니다. 도움말·캘린더 이미지 성공 시 기존 이미지 발송을 유지하며, 텍스트 대체 경로에만 이 정책을 적용합니다.
- 방송·선행공개·영상·쇼츠·커뮤니티·축하·생일 방송·X 스페이스 알림은 단일·묶음·길이와 무관하게 자동 패딩을 넣지 않습니다. 구독 상태·추가/삭제 결과·후보 선택·사용법·오류·졸업 안내도 펼칩니다. `행사 목록`은 구독 상태 안내입니다.
- 머리 문단의 개수·기간·표시 한도·일부 결과 안내와 펼친 화면의 가시 문자·URL·순서를 보존합니다. 일반 방의 `kakaoformat.Render`도 연속 ZWSP 패딩을 보존합니다.
- 사용자 template/채널 override에 직접 넣은 패딩은 제거하거나 중복 삽입하지 않습니다. 저장된 요청의 본문·route·ID와 예약 `PreRenderedMessage`는 다시 렌더하지 않습니다. 새 정책은 새로 렌더하는 메시지에 적용합니다. 실제 카카오톡 화면은 별도 승인된 테스트 방 수신으로 확인합니다.

`!라이브`의 확인 완료 빈 결과는 '현재 방송 중인 멤버가 없습니다.' 한 문장입니다. 미확인 빈 결과는 '현재 방송 상태를 확인할 수 없습니다.'이며 두 결과를 바꾸어 쓰지 않습니다. 공개 방송 범위 설명·조회 미완료 채널·기준 시각은 붙이지 않고 운영 로그로 남깁니다. 멤버 지정 조회는 기존 `CMD_MEMBER_NOT_LIVE`를 유지합니다. migration 220은 217 표준 전역 본문만 바꾸고 사용자 지정 본문·채널 override를 보존합니다.

## 9. 에러·알림 단문 규칙

- `error` 네임스페이스: `❌ `/`⚠️ ` 접두와 키셋은 계약(parity 테스트 고정) — 값만 바꾼다. 본문은 한 문장 서술형 + 마침표, 사용 예시는 둘째 줄 `예) ...`.
- `notify` 네임스페이스: 상태 글리프(✅ ℹ️ 🔔 🔕) 접두 + 한 문장. 상태 상세는 `- ` 행.
- 원인 노출 최소화: 내부 오류는 "무엇이 실패했는지 + 재시도 안내"까지만. 스택·코드·영문 에러 금지.

## 10. 변경 절차 (시드 재작성 규율)

- 기존 마이그레이션 수정 금지. 신규 마이그레이션에 upsert로 재시드한다:
  - 템플릿: `INSERT ... ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now()`
  - 문자열: `INSERT ... ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`
  - `DELETE` 금지. `manifest.txt`에 gapless order로 등록(`check-migration-manifest.sh`가 order 연속성·파일 parity를 강제).
- 테스트 영향은 두 부류로 갈린다 — 반드시 같은 커밋에서 처리한다:
  - **REAL-SEED**(시드 본문을 실제 렌더/조회 — 바꾸면 깨짐): alarm_dispatch 골든, celebration 골든, `store_test.go` 값 핀, `messages_seed_parity_test.go`(키셋·글리프), 라벨 lookup 테스트.
  - **INLINE-TEMPLATE**(테스트 로컬 본문 주입 — 안 깨지지만 미러가 낡음): formatter/llm/outbox 골든의 로컬 본문 상수는 시드의 의도적 미러이므로 lockstep으로 갱신한다.
- channel별 override 행(`channel_id IS NOT NULL`)은 보존이 정책이다 — 새 톤이 자동 적용되지 않으므로 재작성 시 키별 override 목록(`GET /api/holo/templates/:key`의 `overrides`, 조회 SQL `hololive-shared/pkg/repository/queries/template_list_overrides.sql`)으로 대상 방을 기록한다.
- 롤아웃: 템플릿/문자열 캐시는 프로세스별 무기한이므로 `hololive-api`와 `hololive-alarm-worker` **둘 다** 재시작해야 반영된다. (캘린더 PNG 디스크 캐시는 별도 — 렌더러 버전 bump가 담당.)

## 11. 소비자 매트릭스 (2026-07 기준)

| 키 패밀리 | 키 수 | 소비 plane | REAL-SEED 차단 테스트 | lockstep 미러 |
|---|---|---|---|---|
| `CMD_*` 코어 | 22 | bot | — | formatter_{calendar,stats_more,alarm,major_event_profile_more,streams_directory}_test.go 로컬 본문 |
| `CMD_MAJOR_EVENT_*` | 8 | bot + llm | — | formatter_major_event*_test.go, formatter_llm_scheduler_major_member_test.go |
| `CMD_MEMBER_NEWS_*` | 7 | bot + llm | — | formatter_member_news*_test.go, llm 동파일 |
| `OUTBOX_*` | 7 | shared outbox(worker·producer) | outbox 골든의 sample data 임베드 | outbox_header_body_render_test.go 본문 미러 |
| `ALARM_DISPATCH_*` | 2 | alarm-worker | alarm_dispatch_render_golden_test.go | — |
| `CELEBRATION_*` | 2 | alarm-worker | celebration_message_test.go (byte-exact) | — |
| `error` ns | 37 | bot | messages_seed_parity(글리프·키셋) | — |
| `notify` ns | 8 | bot | messages_seed_parity(키셋) | — |
| `org`/`alarmtype`/`newscat`/`social`/`misc` | 25 | bot + llm + worker + outbox | store_test.go 값 핀, 라벨 lookup 테스트 | — |
| `calendar` ns | 8(코드 사용 7 — `overflow_footer`는 단일 페이지 전환으로 미사용 시드 잔존) | bot(render) | calendar_strings_test.go (시드 store byte-equal, 코드 대체 문구 없음), seed_sql_contract_test.go(시드 존재·format 인자 수) | — |
| `timefmt` ns (신규) | 087에서 시드 | bot | seed_sql_contract_test.go(타입 key의 시드 존재·format 인자 수), 세 runtime 기동 검증 | — |
| `karing` ns (087·188) | **미사용 시드 잔존** (alarm-worker Karing 발송 삭제, `DEC-20260926-hololive-karing-egress-disposition`) | — | seed parser 표본(migration 188 행)만 | — |
| `livecard` ns (092) | 4 — **미사용 시드 잔존** (라이브 카드 제거, `!라이브` 텍스트 회귀) | — | — | — |
| `profilecard` ns (093) | 1 — **미사용 시드 잔존** (프로필 카드 제거, `!멤버` 텍스트 회귀) | — | — | — |
| `rankcard` ns (094) | 3 | bot(render 순위 카드) | rank_test.go parity | — |

이미지 카드 표면: `!캘린더`(단일 이미지, 자연 높이 초과 시 compact 비례 축소로 1024x1536 안에 수용)와 `!구독자순위`(단일, top-N)만 유지 — 렌더·전송 실패 시 기존 텍스트로 폴백하며, 카드 문자열은 위 ns가 SSOT다. `!라이브`·`!멤버`는 카카오에서 이미지 내 URL 클릭이 불가해 텍스트 전송으로 회귀했다(라이브·프로필 카드 렌더러 및 SendMultipleImages 배선 제거). 드로잉 프리미티브는 `render/cardkit`, 팔레트는 `render/theme`이 단일 소스.

부록 — 알려진 정합 이슈(개편에서 해소):
- `GetAllTemplateKeys()`/sample data가 `ALARM_DISPATCH_NOTIFICATION{,_GROUP}` 2키를 누락(시드·상수 48 vs 키셋 46) → 키셋·샘플 보강으로 seed-render 게이트에 편입.
- `misc/chzzk_title`은 소비자 없는 orphan — 삭제하지 않고 유지(store_test 핀), 신규 사용 전 재검토.
