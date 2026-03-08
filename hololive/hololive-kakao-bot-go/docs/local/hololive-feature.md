# Hololive KakaoTalk Bot — 기능 구현 A–Ω 가이드

본 문서는 Hololive 기능(라이브/예정/일정/프로필/알림/통계)의 전체 구현 흐름과 데이터·의존성·장애대응을 코드 기준으로 상세히 기술합니다. 코드/경로/명령은 English, 설명은 한국어로 작성합니다.

## Scope
- 포함: 명령 파이프라인, Holodex 연동, 멤버 매칭, 공식 프로필/번역, 방송 알림, 통계, 캐시/DB, 에러·회로차단, 운영/설정.
- 제외: Discord 릴레이(요약만), 체스 기능(간단 표기). 

---

## System Overview
- 목적: 카카오톡에서 Hololive 소속 VTuber의 라이브/예정/개인 일정, 공식 프로필, 방송 알림, 간단 통계를 제공
- 구성:
  - Main Bot: 명령 수신/처리/응답 + 데이터 수집/알림 (`cmd/bot` → `internal/bot`)
  - Relay: Discord → Kakao 브리지(`cmd/discord-relay`, 비핵심)
- 주요 연동: Holodex API, Iris Messenger(HTTP+WebSocket), Valkey(캐시/알람/멤버 DB), PostgreSQL(멤버/통계), Gemini(OpenAI fallback)

아키텍처 개요
- Message In → Iris WS → Parser → Command Registry → Services(Holodex/Member/Alarm/Formatter) → Iris Reply
- Scheduled Out → Alarm ticker → Holodex schedule → Notification group → Iris Reply

---

## Entry Points
- Main Bot: `cmd/bot/main.go`
  - 설정 로드 → 로거/의존성 조립(`internal/app/builder.go`) → `internal/bot.NewBot` → WS 연결/알람 주기/스케줄러 시작

---

## Command Pipeline
1) 수신 및 파싱
- WS 리스너: `internal/bot/bot.go` `setupWebSocket` → `handleMessage`
- 파서: `internal/adapter/message.go` `ParseMessage`
  - 접두사 `BOT_PREFIX`(기본 `!`) 확인 후 토큰화
  - 명령 스위치: 라이브/예정/일정/알람/정보/통계/체스 식별 + 인자 파싱

2) 정규화 및 실행
- 정규화: `internal/bot/bot.go` `normalizeCommand` (예: `alarm_add` → `alarm` with `action=set`)
- 레지스트리: `internal/command/registry.go` (등록/실행)
- 디스패처: `internal/command/dispatcher.go` (순차 실행, 파라미터 클론)

3) 응답 구성
- 포맷터: `internal/adapter/formatter.go` 템플릿 우선 + Kakao ‘전체보기’ 패딩 적용
- 전송: `internal/iris/client.go` `SendMessage`/`SendImage`

---

## Supported Commands
- Live (`!라이브 [멤버]`)
  - 핸들러: `internal/command/live.go`
  - 멤버 미지정 → Holodex live 목록 필터(Hololive only, no HOLOSTARS)
  - 멤버 지정 → 이름→채널 매칭 후 해당 채널 라이브만 출력
- Upcoming (`!예정 [시간]`)
  - 핸들러: `internal/command/upcoming.go`
  - 기본 24h, 1~168h 클램프, 시작 예정 시간 오름차순 포맷
- Schedule (`!일정 <멤버> [일수]`)
  - 핸들러: `internal/command/schedule.go`
  - 졸업 멤버 안내 후 차단, includeLive=true로 24h×days 취득 후 live/upcoming 정렬
- Member Info (`!정보 <멤버>` 또는 `!멤버 <질문>`)
  - 핸들러: `internal/command/member_info.go`
  - 이름/별칭→채널 해상→공식 프로필(raw)+번역(translated) 결합 출력
  - 인자 없을 때 디렉터리 뷰(그룹/우선순위 정렬)
- Alarm (`!알람 추가/제거/목록/초기화 <멤버>…`)
  - 핸들러: `internal/command/alarm.go`
  - 구독 레지스트리 갱신, `다음 방송` 요약 첨부
- Stats (`!구독자순위 [기간]`)
  - 핸들러: `internal/command/stats.go`
  - Ingestion 적재 통계 레포 기반 TOP N 출력

---

## Holodex Integration
API Client (회로차단/키로테이션): `internal/service/holodex/api_client.go`
- 429/403: API Key 로테이션, 시도 한계 시 `KeyRotationError`
- 5xx/네트워크: 지수백오프 + 실패 누적 → Circuit OPEN (`constants.CircuitBreakerConfig`)
- OPEN 상태: 503로 즉시 실패, ResetTimeout 경과 후 half-open

Service: `internal/service/holodex/service.go`
- Live: `GetLiveStreams()` org=Hololive, type=stream, status=live, 캐시키 `holodex:org:Hololive:live`
- Upcoming: `GetUpcomingStreams(hours)` max 168, asc 정렬, 캐시키 `…:upcoming_{h}`
- Channel Schedule: `GetChannelSchedule(channelID,hours,includeLive)` 불러온 live+upcoming 통합 정렬, 필요 시 스크래퍼 폴백
- Channel Search/Info: Holostars/타 Org 제거 후 캐시 저장

Fallback Scraper: `internal/service/holodex/scraper.go`
- 공식 스케줄(https://schedule.hololive.tv/lives/hololive) HTML 파싱
- 멤버명→채널ID 매핑(정적/별칭/부분일치) 후 `Stream` 변환, 구조 변경 감지 시 `StructureChangedError`

Holostars 필터링
- 기준: `channel.Org == "Hololive"` && 이름/영문명/서브조직에 `HOLOSTARS` 미포함 (`isHolostarsChannel`)

---

## Member Matching (Query → Channel)
구현: `internal/service/matcher/matcher.go`
- 데이터 소스
  - 정적: `internal/domain/data/members.json` (임베디드)
  - 동적: Valkey 해시 `hololive:members` (Ingestion에서 Postgres→Valkey 초기화)
- 단계별 전략 (빠름→느림)
  1) Exact Alias Map: 영어/일본어/한국어/별칭 전수 인덱스 해시에서 정확일치
  2) Exact Valkey: 동적 멤버 해시에 정확일치
  3) Partial Static: 단어경계 포함 부분일치(정상화 토큰 길이 기준 가드)
  4) Partial Valkey: 동적 멤버 부분일치
  5) Partial Alias: 모든 별칭 토큰 부분일치
  6) Holodex Search: 외부 검색 후보 리스트 확보
  7) Candidate Selection: 후보 다수 시 Gemini `SelectBestChannel`
- 단기 캐시: 프로세스 내 match 결과 1분 캐시로 재질의 비용 절감

Graduation 가드
- 일정/정보 명령에서 `IsGraduated` 플래그 확인 후 일정 출력 차단(정보는 허용)

---

## Official Profiles & Translation
서비스: `internal/service/member/profile.go`
- 원천: `internal/domain/data/official_profiles_raw.json`
- 사전 번역: `internal/domain/data/official_profiles_ko.json` (있으면 우선)
- 번역 미존재 시: Gemini JSON 생성(`ModelManager.GenerateJSON`)으로 구조화 번역 생성
- 캐시: `hololive:profile:translated:{locale}:{slug}` (Valkey JSON)
- 디렉터리 그룹 추출: 프로필 `Unit/ユニット` 라벨 값 파싱→토큰 분해→별칭 매핑 후 그룹명 정규화

출력 포맷(요약)
- 헤더(표시명 조합: 영어/번역표시/일본어)
- 캐치프레이즈/요약/하이라이트
- 핵심 데이터 n행(최대 8)
- 링크 최대 4개, 공식 URL 표시

---

## Live/Upcoming/Schedule Formatting
포맷터: `internal/adapter/formatter.go`
- Live: 채널/제목/YouTube URL 목록, `🔴 현재 라이브 중 (N개)` 헤더, ‘전체보기’ 페이징 적용
- Upcoming: 채널/제목/한국시간+상대시간, `📅 예정된 방송 (H시간 이내, N개)`
- Schedule: 채널 헤더 + (LIVE/⏰)상태 아이콘 + 시간/URL
- Kakao See-More: 본문 상단 헤더 제거 → 안내문과 함께 패딩 삽입

템플릿: `internal/adapter/templates/*.tmpl`
- 템플릿 실패 시 안전한 Fallback 문자열 빌더 사용

---

## Alarm (Start-Imminent Notifications)
서비스: `internal/service/notification/alarm.go`
- 사용자 명령으로 채널 구독 관리(Add/Remove/List/Clear)
- 주기 체크: Core ticker(`internal/bot/bot.go`), 기본 간격 `CHECK_INTERVAL_SECONDS`(기본 60s)
- 알고리즘
  1) 채널 구독 레지스트리 전체 조회(`alarm:channel_registry`)
  2) 각 채널 24h 스케줄 조회(includeLive)
  3) 라이브/예정 필터 → 목표 분(min) 매칭(기본 30/15/5/1, 설정 가능)
  4) 방별 그룹핑(동일 시각 시작 묶음) → 단건/그룹 포맷 후 전송
  5) 발송 후 `notified:{videoID}`에 스케줄 타임 기록(중복 방지)
- 일정 변경 감지: 이전 `start_scheduled`와 비교하여 ‘앞당김/늦춤’ 메시지 생성
- 다음 방송 캐시: `alarm:next_stream:{channelID}` HSET(`status`, `title`, `video_id`, `start_scheduled`)

Valkey Key 설계(요약)
- 사용자별 알람 Set: `alarm:{roomID}:{userID}` (멤버 채널ID 집합)
- 사용자 인덱스: `alarm:registry` ("room:user" 키 목록)
- 채널→구독자: `alarm:channel_subscribers:{channelID}` (구독자 레지스트리 키 집합)
- 채널 인덱스: `alarm:channel_registry` (알람 대상 채널 집합)
- 다음 방송 HSET: `alarm:next_stream:{channelID}`
- 노티드 플래그: `notified:{videoID}` (중복 발송 방지, TTL 24h)

메시지 구성
- 단건: `internal/adapter/formatter.go` `AlarmNotification`
- 다건: `AlarmNotificationGroup` (채널명/제목/URL 정렬, 헤더에 ‘곧 시작/진행 중’ 등 표기)

---

## Data & Domain Model
- `internal/domain/stream.go` — Stream 상태(Live/Upcoming/Past), YouTube URL, 시작시각 유틸
- `internal/domain/channel.go` — Channel(Org/Suborg/Group), Hololive 판별/표시명
- `internal/domain/member.go` — Member(영/일/한/별칭/졸업), 임베디드 JSON 로딩
- `internal/domain/command.go` — CommandType, ParseResults, ChannelSelection 구조

---

## Caching Strategy
- 1차: Valkey JSON/Hash/Set 기반 TTL 캐시(`internal/constants/constants.go`)
  - Live/Upcoming/Channel/Schedule/Search: 5–20분 
  - NextStreamInfo/Notified: 60분–24시간
- 2차: 프로세스 단기 캐시(매칭 결과 등)
- 캐시 미스 시 API 호출, 회로차단 상태면 Scraper 폴백(채널 스케줄)

---

## Error Handling & Resilience
- Holodex API 장애/레이트: 키로테이션 → 지수백오프 → Circuit Open → Scraper 폴백
- Iris 송신 실패: 클라이언트 레벨 재시도(백오프), 사용자 메시지는 일반화하여 전달
- 알람 중복: `notified:{videoID}` 키로 억제 + 다음방송 캐시 보존(깜빡임 방지)
- 졸업 멤버: 일정 조회 차단, 정보 조회는 허용(주의 문구)

---

## Configuration
필수/권장 환경 변수(README와 동일, 확장 포함)

```env
# Iris Server
IRIS_BASE_URL=http://localhost:3000
IRIS_WS_URL=ws://localhost:3000/ws

# Kakao
KAKAO_ROOMS=홀로라이브 알림방

# Holodex
HOLODEX_API_KEY_1=...
HOLODEX_API_KEYS=...,..., ...   # 일괄등록(선택)

# Valkey
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_DB=0

# Postgres
POSTGRES_HOST=localhost
POSTGRES_USER=holo_user
POSTGRES_DB=holo_oshi_db

# AI
GEMINI_API_KEY=...
OPENAI_API_KEY=...               # fallback

# Bot
BOT_PREFIX=!
BOT_SELF_USER=iris

# Notification / Core soft-gate
NOTIFICATION_ADVANCE_MINUTES=30,15,5,1
CHECK_INTERVAL_SECONDS=60
CORE_MEMBER_HASH_SOFT_READY=true
CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS=15
CORE_MEMBER_HASH_SOFT_MIN_COUNT=10
```

검증: `internal/config/config.go` `Validate()`에서 URL/키/체스/타임아웃 등 유효성 체크

---

## Operations (Runbook)
- Bot 실행: `./scripts/bot.sh start`
- 상태: `./scripts/bot.sh status` / 로그: `tail -f logs/bot.log`
- 재시작: `./scripts/bot.sh restart`
- 종료: `./scripts/bot.sh stop`

점검 체크리스트
- Valkey 연결/키 TTL: `holodex:*`, `alarm:*`, `hololive:members` 존재 확인
- Holodex 응답/레이트: 로그 `Holodex API key pool configured`/Circuit open 경고
- 알람 효과: `alarm:channel_registry`/`alarm:next_stream:*` 갱신 관찰

---

## Testing
- 유닛 테스트: `go test ./internal/...`
- 도메인 단위: `internal/domain/stream_test.go` 등
- 통합(수동): 값이 있는 HOLODEX 키/Valkey로 라이브/예정 응답 확인, 알람 트리거 간격 단축 후 검증

---

## Extensibility
- 명령 추가: `internal/command`에 핸들러 추가 → `internal/bot/bot.go` `initializeCommands()`에서 등록
- 외부 소스 추가: Holodex 인터페이스 유사 서비스로 병합 결과 구성(우선순위/결합 전략 필요)
- 멤버 스키마 확장: MemberRepository/CacheService에 필드 반영 + 프로필 포맷 조건부 확장
- 템플릿 커스텀: `internal/adapter/templates/*.tmpl` 조정(헤더/패딩 규칙 유지)

---

## Known Edge Cases
- 유사명 충돌(동명이/별칭 다수): 단계적 매칭→Gemini 선택으로 완화, 신뢰도 낮을 때 추가 질의(Clarification) 고려 가능
- 공식 스케줄 HTML 변화: Scraper 구조 변경 감지시 경고 로그 + Holodex 정상화까지 임시 품질 저하
- 라이브→예정 전환 타이밍: NextStream 캐시 보존 정책(`shouldPreserveCache`)으로 깜빡임 최소화

---

## Security & Compliance
- API 키는 `.env`/환경변수로만 주입, VCS 미커밋
- Valkey/Postgres 자격 증명은 로컬 개발 범위에서만 평문 허용, 배포 환경은 시크릿/바운드 볼륨 사용
- 외부 HTML 스크래핑은 User-Agent 지정, 타임아웃/빈도 제한 준수

---

## Performance Notes
- Holodex 호출 최소화: Valkey TTL 캐시, 스케줄/라이브 5분 캐시, 검색 10분 캐시
- 매칭 비용 절감: Alias 맵/Valkey 동적 DB/프로세스 캐시 + Holodex Search 지연 실행
- 포맷 최적화: Kakao ‘전체보기’ 패딩으로 장문 안전 출력

---

## Glossary (A–Ω)
- A — Alarm: 방송 임박 알림(분 단위 타겟)
- C — Circuit Breaker: Holodex API 실패 보호
- D — Directory: 멤버 디렉터리(그룹/우선순위)
- F — Fallback: 스크래퍼 대체 경로
- H — Holodex: 스트림/채널 메타 API
- M — Matcher: 이름/별칭/부분일치/AI 선택
- N — Next Stream Cache: 다음 방송 요약 캐시
- P — Profile: 공식 프로필+번역
- R — Valkey: 캐시/알람/멤버 해시DB
- S — Schedule: 개인 일정(라이브 포함/제외)
- U — Upcoming: 예정 방송(전채널)
- Ω — Operations: 실행/상태/로그/점검

---

## Change Log (문서)
- 2025-10-25: 최초 작성(코드 베이스 전수 리딩 기반)

---

## Bundling & Aggregation Mechanics (with code)

이 섹션은 “무엇을 어떤 기준으로 묶는가(aggregate)”를 코드 단위로 설명합니다.

### 1) Command 정규화와 순차 실행
- 목적: `alarm_add`/`alarm_remove` 등 파생 타입을 하나의 `alarm` 키로 “묶어” 실행 순서를 단순화
- 어디: `internal/bot/bot.go` `normalizeCommand`

```go
// internal/bot/bot.go: normalizeCommand
func (b *Bot) normalizeCommand(cmdType domain.CommandType, params map[string]any) (string, map[string]any) {
    typeStr := strings.ToLower(cmdType.String())

    if strings.HasPrefix(typeStr, "alarm_") {
        action := strings.TrimPrefix(typeStr, "alarm_")
        newParams := make(map[string]any)
        for k, v := range params { newParams[k] = v }
        newParams["action"] = action
        return "alarm", newParams
    }

    if typeStr == "alarm" {
        if _, hasAction := params["action"]; !hasAction {
            newParams := make(map[string]any)
            for k, v := range params { newParams[k] = v }
            newParams["action"] = "list"
            return "alarm", newParams
        }
    }
    return typeStr, params
}
```

- 실행: `Dispatcher`가 정규화된 키로 순차 실행

```go
// internal/command/dispatcher.go: Publish (sequential aggregation)
func (d *sequentialDispatcher) Publish(ctx context.Context, cmdCtx *domain.CommandContext, events ...CommandEvent) (int, error) {
    if d == nil || d.registry == nil || d.normalize == nil { return 0, nil }
    executed := 0
    for _, event := range events {
        if event.Type == domain.CommandUnknown { continue }
        normalizedParams := cloneParams(event.Params)
        key, params := d.normalize(event.Type, normalizedParams)
        if err := d.registry.Execute(ctx, cmdCtx, key, params); err != nil {
            return executed, err
        }
        executed++
    }
    return executed, nil
}
```

### 2) 방송 알림 묶음 (Alarm Notification Grouping)
- 목적: 같은 방(room)에서 동일 시각(분 단위) 시작 방송들을 하나의 메시지로 묶어 소음 최소화
- 어디: `internal/bot/bot.go` `groupAlarmNotifications`, `buildAlarmGroupKey`

```go
// internal/bot/bot.go: groupAlarmNotifications
func groupAlarmNotifications(notifications []*domain.AlarmNotification) []*alarmNotificationGroup {
    if len(notifications) == 0 { return []*alarmNotificationGroup{} }
    groups := make([]*alarmNotificationGroup, 0)
    index := make(map[string]int)
    for _, notif := range notifications {
        if notif == nil { continue }
        key := buildAlarmGroupKey(notif)
        if idx, ok := index[key]; ok {
            group := groups[idx]
            group.notifications = append(group.notifications, notif)
            if notif.MinutesUntil >= 0 && (group.minutesUntil < 0 || notif.MinutesUntil < group.minutesUntil) {
                group.minutesUntil = notif.MinutesUntil
            }
            continue
        }
        group := &alarmNotificationGroup{ roomID: notif.RoomID, minutesUntil: notif.MinutesUntil, notifications: []*domain.AlarmNotification{notif} }
        groups = append(groups, group)
        index[key] = len(groups) - 1
    }
    return groups
}

// internal/bot/bot.go: buildAlarmGroupKey
func buildAlarmGroupKey(notif *domain.AlarmNotification) string {
    if notif == nil { return "" }
    if notif.Stream != nil && notif.Stream.StartScheduled != nil {
        scheduled := notif.Stream.StartScheduled.Truncate(time.Minute)
        return fmt.Sprintf("%s|scheduled|%d", notif.RoomID, scheduled.Unix())
    }
    return fmt.Sprintf("%s|minutes|%d", notif.RoomID, notif.MinutesUntil)
}
```

- 효과: 동일 분(minute) 기준으로 그룹 키를 형성하여 병합 → 다건일 때 `Formatter.AlarmNotificationGroup` 사용

### 3) 알람 레지스트리 묶음 (Subscribers/Channels Indexing)
- 목적: 채널 단위로 구독자 집합을 유지하고, 전체 대상 채널을 인덱싱하여 스캔 비용 최소화
- 어디: `internal/service/notification/alarm.go` `AddAlarm`/`RemoveAlarm`/`GetUserAlarms`

```go
// internal/service/notification/alarm.go: AddAlarm (registry aggregation)
func (as *AlarmService) AddAlarm(ctx context.Context, roomID, userID, channelID, memberName string) (bool, error) {
    alarmKey := as.getAlarmKey(roomID, userID)
    added, err := as.cache.SAdd(ctx, alarmKey, []string{channelID})
    if err != nil { return false, err }

    registryKey := as.getRegistryKey(roomID, userID)
    _, _ = as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey})

    channelSubsKey := as.channelSubscribersKey(channelID)
    _, _ = as.cache.SAdd(ctx, channelSubsKey, []string{registryKey})
    _, _ = as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{channelID})

    _ = as.CacheMemberName(ctx, channelID, memberName)
    return added > 0, nil
}

// key helpers
func (as *AlarmService) getAlarmKey(roomID, userID string) string { return AlarmKeyPrefix + roomID + ":" + userID }
func (as *AlarmService) getRegistryKey(roomID, userID string) string { return roomID + ":" + userID }
func (as *AlarmService) channelSubscribersKey(channelID string) string { return ChannelSubscribersKeyPrefix + channelID }
```

- 설계:
  - 사용자→채널 구독: `alarm:{room}:{user}` Set
  - 채널→구독자 인덱스: `alarm:channel_subscribers:{channelID}` Set
  - 전체 대상 채널: `alarm:channel_registry` Set
  - 다음 방송 요약: `alarm:next_stream:{channelID}` HSET

### 4) 멤버 디렉터리 묶음 (Group Classification)
- 목적: 프로필의 `Unit/ユニット` 값을 파싱하여 그룹(예: Myth, Promise, holoX 등)으로 묶어 디렉터리 구성
- 어디: `internal/command/member_info.go` `memberGroups` → `extractUnitValues` → `normalizeMemberGroup`

```go
// internal/command/member_info.go: memberGroups (핵심 로직)
func (c *MemberInfoCommand) memberGroups(ctx context.Context, member *domain.Member) []string {
    profile, translated, err := c.deps.OfficialProfiles.GetWithTranslation(ctx, member.Name)
    if err != nil { return nil }
    rawValues := extractUnitValues(profile, translated)
    if len(rawValues) == 0 { return nil }
    normalized := make([]string, 0, len(rawValues))
    seen := make(map[string]bool)
    for _, raw := range rawValues {
        for _, token := range splitGroupTokens(raw) {
            name := normalizeMemberGroup(token)
            if name != "" && !seen[name] {
                normalized = append(normalized, name)
                seen[name] = true
            }
        }
    }
    return normalized
}

func normalizeMemberGroup(name string) string {
    trimmed := strings.TrimSpace(name)
    if idx := strings.IndexAny(trimmed, "（("); idx != -1 { trimmed = strings.TrimSpace(trimmed[:idx]) }
    if mapped, ok := memberDirectoryGroupAliases[trimmed]; ok { return mapped }
    if strings.HasPrefix(trimmed, "ホロライブEnglish -") { suffix := strings.Trim(trimmed[len("ホロライブEnglish -"):], "-"); if suffix != "" { return suffix } }
    if strings.HasPrefix(trimmed, "hololive English") {
        suffix := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "hololive English")), "-")
        if suffix != "" { return suffix }
    }
    return trimmed
}
```

### 5) 채널 후보 묶음 → 최적 선택 (Gemini 보조)
- 목적: Holodex 검색으로 얻은 다수 후보를 “하나의 채널”로 결정
- 어디: `internal/service/matcher/matcher.go` `selectBestFromCandidates`

```go
// internal/service/matcher/matcher.go: selectBestFromCandidates
func (mm *MemberMatcher) selectBestFromCandidates(ctx context.Context, query string, channels []*domain.Channel) (*domain.Channel, error) {
    if len(channels) == 0 { return nil, nil }
    if len(channels) == 1 { return channels[0], nil }
    if mm.selector == nil { return channels[0], nil }
    selected, err := mm.selector.SelectBestChannel(ctx, query, channels)
    if err != nil { return nil, nil }
    return selected, nil
}
```

### 6) 목록 묶음 출력 (Live/Upcoming)
- 어디: `internal/adapter/formatter.go` `FormatLiveStreams`, `UpcomingStreams`

```go
// internal/adapter/formatter.go: FormatLiveStreams (요약)
func (f *ResponseFormatter) FormatLiveStreams(streams []*domain.Stream) string {
    data := liveStreamsTemplateData{Count: len(streams)}
    // N개 항목 → 템플릿 데이터로 묶음 → Kakao See-More 패딩 안내문과 결합
    instruction := ""
    if data.Count > 0 { instruction = fmt.Sprintf("🔴 현재 라이브 중 (%d개)", data.Count) }
    if rendered, err := executeFormatterTemplate("live_streams.tmpl", data); err == nil {
        if data.Count == 0 { return rendered }
        return util.ApplyKakaoSeeMorePadding(stripLeadingHeader(rendered, instruction), instruction)
    }
    // 템플릿 실패 시 Fallback 빌더 사용
    return f.fallbackLiveStreams(data)
}
```

### 7) Holodex Hololive 필터 묶음
- 어디: `internal/service/holodex/service.go` `filterHololiveStreams`

```go
// internal/service/holodex/service.go: filterHololiveStreams
func (h *HolodexService) filterHololiveStreams(streams []*domain.Stream) []*domain.Stream {
    filtered := make([]*domain.Stream, 0, len(streams))
    for _, stream := range streams {
        if stream.Channel == nil { continue }
        channel := stream.Channel
        if channel.Org == nil || *channel.Org != "Hololive" { continue }
        if h.isHolostarsChannel(channel) { continue }
        filtered = append(filtered, stream)
    }
    return filtered
}
```

---

## How-To: 운영 절차 & 대표 시나리오 (with code pointers)

### 권장 운영 순서(동시 운영)
1) Ingestion 먼저 실행: 멤버 해시 DB 초기화 및 Ready 플래그 세팅
- 파일: `cmd/bot-ingestion/main.go`, `internal/ingestion/app/app.go`
- 핵심: `cache.InitializeMemberDatabase(...)`, `_ = cache.SetMemberReady(...)`

2) Core 실행: WS 연결 성공 후 알람 ticker 시작
- 파일: `cmd/bot-core/main.go`, `internal/bot/bot.go`
- 핵심: `setupWebSocket()`, `startAlarmChecker()`

3) 상태 점검
- `scripts/bot.sh status`, Valkey 키/TTL 확인(`holodex:*`, `alarm:*`, `hololive:members`)

### 알람 기능 활성/검증 절차
1) 사용자가 “!알람 추가 <멤버>” 전송
   - Parser: `internal/adapter/message.go` `tryAlarmCommand`
   - Normalize: `normalizeCommand` → `alarm + action=set`
   - Matcher: `MemberMatcher.FindBestMatch`로 채널ID 해상
   - Add: `AlarmService.AddAlarm` → 레지스트리/구독자 집합/채널 인덱스 갱신, `CacheMemberName`
   - 응답: `Formatter.FormatAlarmAdded` (다음 방송 요약 포함)

2) 알람 tick 발생(`performAlarmCheck`)
   - `AlarmService.CheckUpcomingStreams` → 채널 24h 스케줄 수집(includeLive)
   - `filterUpcomingStreams`로 목표분(min) 매칭
   - `groupAlarmNotifications`로 방/시각 묶음 → 단건/그룹 포맷
   - 전송 후 `MarkAsNotified`로 중복 발송 억제

### 대표 시나리오 트레이스(요약 + 코드 경로)
Scenario: “!알람 추가 페코라” → 방송 임박 알림 수신
1) `adapter/message.go` Parse → Type=alarm, Params={ action=set, member="페코라" }
2) `bot.go` normalizeCommand → ("alarm", { action=set, member=... })
3) `matcher/matcher.go` FindBestMatch → channelID 해상(에일리어스→정적/동적→Holodex 검색→Gemini 선택)
4) `notification/alarm.go` AddAlarm → 레지스트리/구독자/채널 인덱스 업데이트
5) 응답 `formatter.go` FormatAlarmAdded → 다음 방송 요약 `GetNextStreamInfo`
6) 주기 tick `bot.go` performAlarmCheck → `CheckUpcomingStreams` → `groupAlarmNotifications` → 메시지 전송 → `MarkAsNotified`

### 실전 코드 스냅샷(핵심 함수 묶음)
- 정규화: `internal/bot/bot.go: normalizeCommand`
- 그룹핑: `internal/bot/bot.go: groupAlarmNotifications / buildAlarmGroupKey`
- 레지스트리: `internal/service/notification/alarm.go: AddAlarm / getAlarmKey / channelSubscribersKey`
- 매칭/선택: `internal/service/matcher/matcher.go: selectBestFromCandidates`
- 포맷: `internal/adapter/formatter.go: FormatLiveStreams / AlarmNotificationGroup`
- 필터: `internal/service/holodex/service.go: filterHololiveStreams`
