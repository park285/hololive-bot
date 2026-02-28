# Member News Feature (v4) — Fresh Context Execution Handoff

## 목적
홀로라이브 카카오톡 봇에 `LLM 기반 구독멤버 뉴스` 기능을 추가한다.

- 즉시 조회: `!뉴스 [이번주|이번달]`
- 구독 제어: `!뉴스알림 켜기|끄기|상태`
- 자동 발송: 매주 월요일 09:00 KST
- 요약: Cliproxy + Exa 보강, 스키마 강제, 실패 시 deterministic fallback

---

## 최종 확정 의사결정 (변경 금지)

1. **뉴스 대상 멤버**: `alarms`(기존 방송 알람) 기반 room별 멤버 재사용
2. **뉴스 구독 on/off 저장소**: `member_news_subscriptions` (Postgres) + Valkey write-through
3. **기간 조건**
   - weekly: `최근 7일 + 향후 21일`
   - monthly: `당월(1일~말일)`
4. **소스 정책**: 공식 우선 + 커뮤니티 보조
5. **source_url 필수**: 없는 항목은 drop
6. **x.com 검증**: `configs/hololive_official_x_accounts.json` allowlist 필수
7. **메시지 길이**: 핵심 5건 + 나머지 요약
8. **자동 발송 중복 방지 키**
   - execution lock: `membernews:lock:weekly:{weekKey}` TTL `10m`
   - room claim: `membernews:sent:weekly:{weekKey}:{roomID}` TTL `8d`

---

## 구현 범위 (파일 단위)

### 1) Domain / Parser / Router
- `internal/domain/command.go`
  - `CommandMemberNews`
  - `CommandMemberNewsSubscription`
- `internal/adapter/message.go`
  - `tryMemberNewsCommand`
  - `tryMemberNewsSubscriptionCommand`
  - **순서**: news 계열이 `tryMajorEventCommand`보다 앞
- `internal/bot/bot.go`
  - `normalizeCommand`에 `news_subscription_* -> news_subscription(action)` 추가
  - command 등록 추가
- `internal/bot/command_router.go`
  - 동일 normalize 동작 반영

### 2) 신규 서비스 패키지
- `internal/service/membernews/`
  - `repository.go` (구독 CRUD)
  - `service.go` (room 대상 뉴스 생성)
  - `scheduler.go` (주간 자동 발송)
  - `summarizer.go` (LLM + schema + validator + fallback)
  - `filter.go` (기간/멤버/정렬/카테고리)
  - `source_validator.go` (도메인/x allowlist)

### 3) Command
- `internal/command/member_news.go`
- `internal/command/member_news_subscription.go`
- `internal/command/command.go`
  - `Dependencies`에 member news service 주입

### 4) Formatter / Template
- `internal/adapter/formatter_member_news.go`
- `internal/adapter/messages.go`
  - news 관련 에러 메시지 상수 추가
- `internal/domain/notification_template.go`
  - TemplateKey 추가
- `internal/domain/template_sample_data.go`
  - 샘플 데이터/유효키 목록 추가

### 5) App Wiring
- `internal/bot/deps.go`
- `internal/app/providers.go`
- `internal/app/bootstrap.go`
- `internal/app/runtime.go`
  - scheduler start/stop 연결

### 6) Migration / Config
- `scripts/migrations/030_add_member_news_subscriptions.sql`
- `scripts/migrations/031_seed_member_news_templates.sql`
- `configs/hololive_official_x_accounts.json` (신규)

---

## DB 명세 (고정)

### `member_news_subscriptions`
```sql
CREATE TABLE IF NOT EXISTS member_news_subscriptions (
  id SERIAL PRIMARY KEY,
  room_id VARCHAR(255) UNIQUE NOT NULL,
  room_name VARCHAR(255),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_member_news_subscriptions_created_at
ON member_news_subscriptions(created_at);
```

### SQL 동작 규칙
- subscribe:
```sql
INSERT INTO member_news_subscriptions (room_id, room_name)
VALUES ($1, $2)
ON CONFLICT (room_id) DO UPDATE
SET room_name = COALESCE(EXCLUDED.room_name, member_news_subscriptions.room_name),
    updated_at = NOW();
```
- unsubscribe: `DELETE FROM member_news_subscriptions WHERE room_id = $1`
- isSubscribed: `SELECT EXISTS(...)`
- listRooms: `ORDER BY created_at ASC`

---

## Valkey 키 명세 (고정)
- `membernews:rooms` (SET room_id)
- `membernews:room_names` (HASH room_id -> room_name)

### write-through 규칙
- subscribe 성공 후 `SADD` + `HSET`
- unsubscribe 성공 후 `SREM` + `HDEL`
- warm-up: 부팅 시 DB 목록 전량 적재
- Valkey 쓰기 실패는 warn 로그, 기능은 Postgres 기준 지속

---

## 뉴스 후보 추출/정렬 규칙

### 후보 수집
1. news 후보: `major_events` (`status='active' AND type='news'`)
   - 기간 필터는 `pub_date` 우선
   - `pub_date` 없으면 `event_start_date` 대체
2. event 후보: `major_events` (`status='active' AND type='event'`)
   - 기간 필터는 `event_start_date`

### 룸 멤버 필터
- `alarms`에서 room_id 기준 멤버 집합 조회
- title/description/members 필드에 대한 룰 기반 매칭

### category 매핑 우선순위
`birthday_live > solo_live > collab > event > goods > other`

- birthday_live: `生誕`, `생일`, `birthday`
- solo_live: `ソロライブ`, `solo live`, `단독 라이브`
- collab: `コラボ`, `콜라보`, `collaboration`
- goods: `グッズ`, `굿즈`, `merchandise`
- event: `fes`, `expo`, `live`, `concert`, `event`

### source 신뢰도
- official: `hololive.hololivepro.com`, `hololivepro.com`, `cover-corp.com`, `youtube.com(공식 채널)`, `x.com(공식 계정)`
- media: allowlist 언론 도메인
- community: 그 외

### 최종 정렬
`날짜 오름차순 -> source 신뢰도(official>media>community) -> category 우선순위`

---

## LLM 설계 (고정)

### JSON Schema (required)
- `period`: `weekly | monthly`
- `headline`: string
- `top_items`: array(max 5)
  - required: `member`, `category`, `title`, `date_text`, `summary`, `source_url`
- `more_summary`: string
- `omitted_count`: integer (>=0)

### HARD validator
- `source_url` 파싱 실패 => drop
- 허용 도메인 불일치 => drop
- `x.com` 계정이 allowlist 불일치 => drop
- 커뮤니티 단독 근거(공식/언론 corroboration 없음) => drop

### fallback
다음 조건이면 deterministic fallback 사용:
- LLM 호출 실패
- JSON schema 파싱 실패
- validator 통과 item 0건

fallback 출력 형식:
- 상위 5건: `[날짜] [멤버] [제목] [카테고리] [링크]`
- 나머지: `외 N건`

---

## 프롬프트 템플릿 (초안)

### System Prompt (요약)
- 역할: hololive member-news curator
- 제약:
  - 사실 기반, 추측 금지
  - 한국어 요약
  - source_url 필수
  - 입력 기간/입력 후보 밖 내용 금지
  - JSON schema strict 준수

### User Prompt 템플릿
- 오늘 날짜
- 기간(`weekly`/`monthly`)
- room 구독 멤버 목록
- 후보 이벤트 JSON
- Exa 검색 컨텍스트
- “schema 외 텍스트 금지” 문구

---

## 테스트 체크리스트 (완료 기준)

### Parser
- `!뉴스`, `!뉴스 이번주`, `!뉴스알림 켜기/끄기/상태`
- `!행사알림`이 뉴스로 오인식되지 않음

### Command
- deps nil/error wrapping
- 멤버 0건 안내 메시지
- service 오류 시 SendError

### Repository
- subscribe idempotent
- unsubscribe 후 isSubscribed=false
- listRooms created_at asc

### Scheduler
- 월 09:00 KST next run
- lock 미획득 시 skip
- claim skip 동작
- partial fail => mark 미실행
- success/all-skip => mark 실행

### Summarizer
- schema 성공
- source_url 누락 drop
- 허용 도메인 위반 drop
- x.com 비공식 계정 drop
- LLM 실패 fallback non-empty

### Formatter
- top5 + more_summary + 링크 출력
- 템플릿 렌더 실패 fallback 에러문구

---

## Fresh Context에서 바로 시작하는 실행 순서

```bash
cd /home/kapu/gemini/llm

# 구조 확인 (AGENTS 규칙)
tree -L 3
tree -L 4 hololive-kakao-bot-go

# 구현 후 검증
make -C hololive-kakao-bot-go fmt
make -C hololive-kakao-bot-go lint
make -C hololive-kakao-bot-go test
```

권장 커밋 단위:
1. domain/parser/router/command skeleton
2. DB migration + repository + Valkey sync
3. membernews service/scheduler
4. LLM summarizer + validator + fallback
5. formatter/template/config
6. tests + fixups

