# Settlement Bot (정산 봇) - Design Spec

## Overview

카카오톡 단체방에서 매달 고정 구독료 정산을 관리하는 봇 커맨드.
기존 hololive-bot에 커맨드를 추가하고, Iris에 멘션 트리거를 지원한다.

## Requirements

- **고정 멤버 4명**: 비포, 심심이, 돈이좋아요, 겜데브
- **고정 금액**: 36,000원/월 (9,000원/인)
- **갱신일**: 매달 18일
- **알람**: 매달 17일 자동 알람 (미입금자 목록)
- **체크**: 당사자가 `@카푸봇 정산완료`로 본인 체크
- **조회**: `!정산`으로 현재 상태 조회
- **단일 정산**: 이 구독 하나만 관리 (범용 아님)

## Architecture

### Layer 1: Iris (Kotlin) - 멘션 트리거 추가

**파일**: `CommandParser.kt`

현재 `!`/`/` prefix만 `WEBHOOK`으로 라우팅.
`@카푸봇`으로 시작하는 메시지도 `WEBHOOK`으로 라우팅 추가.

```kotlin
// 변경 전
normalizedText.startsWith(PREFIX_BANG) || normalizedText.startsWith(PREFIX_SLASH) -> CommandKind.WEBHOOK

// 변경 후
normalizedText.startsWith(PREFIX_BANG) || normalizedText.startsWith(PREFIX_SLASH) || isMentionCommand(normalizedText) -> CommandKind.WEBHOOK
```

`isMentionCommand`: `Configurable.botName`(런타임 config)을 참조하여
`@{botName}` prefix 매칭. 하드코딩 회피.

### Layer 2: Go Bot - 메시지 파싱

**파일**: `internal/adapter/message.go`, `message_parser_settlement.go` (신규)

`MessageAdapter.ParseMessage()`에서 `@카푸봇` prefix 감지 시
`!` prefix 파싱 경로와 별도로 멘션 파싱 경로 추가.

#### 멘션 파싱 흐름

```
"@카푸봇 정산완료" → extractMentionCommand("@카푸봇", raw)
  → command="정산완료", args=[]
  → trySettlementMentionCommand() → CommandSettlementPaid
```

#### 커맨드 파싱 흐름 (prefix `!`)

```
"!정산" → extractCommandText("!", raw)
  → command="정산", args=[]
  → trySettlementCommand() → CommandSettlementStatus
```

### Layer 3: CommandType 추가

**파일**: `hololive-shared/pkg/domain/command.go`

```go
CommandSettlementStatus CommandType = "settlement_status"  // !정산
CommandSettlementPaid   CommandType = "settlement_paid"    // @카푸봇 정산완료
```

### Layer 4: Command Handler

**파일**: `internal/command/handler_settlement.go` (신규)

```go
type SettlementCommand struct {
    BaseCommand
    repo SettlementRepository
}
```

#### Execute 분기

| params["action"] | 동작 |
|------------------|------|
| `"status"` | 현재 달 정산 상태 조회 → 포맷 후 응답 |
| `"paid"` | cmdCtx.UserName 기준 본인 체크 → 확인 응답 |

### Layer 5: Repository (DB)

**파일**: `internal/service/settlement/repository.go` (신규)

```go
type Repository struct {
    pool   *pgxpool.Pool
    logger *slog.Logger
}

type SettlementRepository interface {
    GetCurrentCycle(ctx context.Context, roomID string) (*SettlementCycle, error)
    EnsureCurrentCycle(ctx context.Context, roomID string) (*SettlementCycle, error)
    MarkPaid(ctx context.Context, cycleID int, memberName string) error
    GetUnpaidMembers(ctx context.Context, cycleID int) ([]string, error)
}
```

### Layer 6: Domain Model

**파일**: `internal/service/settlement/types.go` (신규)

```go
type SettlementCycle struct {
    ID        int
    RoomID    string
    Year      int
    Month     int
    DueDay    int       // 18
    Amount    int       // 36000
    PerPerson int       // 9000
    Members   []MemberPayment
    CreatedAt time.Time
}

type MemberPayment struct {
    MemberName string
    PaidAt     *time.Time // nil = 미입금
}
```

### Layer 7: DB Migration

**파일**: `scripts/migrations/038_create_settlement.sql`

```sql
CREATE TABLE IF NOT EXISTS settlements (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(100) NOT NULL,
    year INT NOT NULL,
    month INT NOT NULL,
    due_day INT NOT NULL DEFAULT 18,
    amount INT NOT NULL DEFAULT 36000,
    per_person INT NOT NULL DEFAULT 9000,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (room_id, year, month)
);

CREATE TABLE IF NOT EXISTS settlement_payments (
    id SERIAL PRIMARY KEY,
    settlement_id INT NOT NULL REFERENCES settlements(id) ON DELETE CASCADE,
    member_name VARCHAR(100) NOT NULL,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (settlement_id, member_name)
);

CREATE INDEX idx_sp_settlement_unpaid
    ON settlement_payments (settlement_id)
    WHERE paid_at IS NULL;
```

### Layer 8: Formatter

**파일**: `internal/adapter/formatter_settlement.go` (신규)

#### 정산 상태 응답 (`!정산`)

```
[정산] 2026년 3월 (36,000원 / 4명)

인당 9,000원 | 갱신일 18일

[완료] 비포 (03/15)
[완료] 심심이 (03/16)
[미입금] 돈이좋아요
[미입금] 겜데브
```

#### 정산 완료 응답 (`@카푸봇 정산완료`)

```
[정산] 비포님 정산 완료 (2/4)
```

#### 미입금 알람 (17일 자동)

```
[정산] 내일(18일) 갱신일입니다.

미입금: 돈이좋아요, 겜데브
인당 9,000원 | 총 36,000원
```

### Layer 9: Settlement Scheduler

**파일**: `internal/service/settlement/scheduler.go` (신규)

기존 `RuntimeScheduler`와 별도의 간단한 daily 체크 루프.
`Start(ctx)` → goroutine, 매 시간 체크.

```go
func (s *Scheduler) check(ctx context.Context) {
    now := time.Now().In(kst)
    // 17일이고 아직 알람 안 보냈으면 → 미입금자 알람 발송
    // 18일 넘었으면 → 다음 달 사이클 자동 생성
}
```

중복 발송 방지: `settlement_alarm_sent` Valkey 키 (24h TTL).

### Layer 10: Bootstrap / DI

**파일**: `internal/app/bootstrap_services.go`, `bootstrap_bot.go`

- `SettlementRepository` 생성 → `Dependencies`에 주입
- `SettlementCommand` 팩토리 등록
- `SettlementScheduler` 생성 → `RuntimeScheduler`와 병렬 실행

## Data Flow

### 정산 완료 체크

```
카카오톡 → "@카푸봇 정산완료"
  → Iris CommandParser (멘션 → WEBHOOK)
  → H2cDispatcher → Go Bot webhook
  → MessageAdapter.ParseMessage() → CommandSettlementPaid
  → SettlementCommand.Execute("paid")
  → Repository.EnsureCurrentCycle() + MarkPaid(userName)
  → Formatter → "비포님 정산 완료 (2/4)"
  → Iris → 카카오톡
```

### 자동 알람 (매달 17일)

```
SettlementScheduler.check()
  → 17일 확인 + 중복 체크 (Valkey)
  → Repository.GetUnpaidMembers()
  → Formatter → 미입금 알람 메시지
  → SendMessage() → Iris → 카카오톡
```

## Member Name Matching

`@카푸봇 정산완료` 시 `cmdCtx.UserName`(카카오톡 닉네임)을
settlement_payments.member_name과 매칭해야 한다.

**방법**: 닉네임 → 멤버명 매핑 테이블 (settlements 내부).

```sql
CREATE TABLE IF NOT EXISTS settlement_members (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(100) NOT NULL,
    member_name VARCHAR(100) NOT NULL,  -- 정산용 이름 (비포, 심심이 등)
    kakao_user_id VARCHAR(100),         -- 카카오톡 user_id (최초 매칭 시 저장)
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (room_id, member_name)
);
```

최초 `@카푸봇 정산완료` 시:
1. `kakao_user_id`로 매칭 시도
2. 매칭 실패 시 `cmdCtx.UserName`과 `member_name` 퍼지 매칭
3. 매칭 성공 시 `kakao_user_id` 저장 (이후 정확한 매칭)
4. 매칭 실패 시 에러: "등록된 멤버가 아닙니다"

## Testing

- `handler_settlement_test.go`: status/paid 분기, 미등록 멤버 에러
- `message_parser_settlement_test.go`: 멘션 파싱, `!정산` 파싱
- `repository_test.go`: cycle 생성, 중복 방지, mark paid, unpaid 조회
- `scheduler_test.go`: 17일 알람 트리거, 중복 방지, 사이클 자동 생성
- `CommandParserTest.kt`: `@카푸봇` 멘션 → WEBHOOK 라우팅

## Files to Create/Modify

### New Files (Go)
- `internal/service/settlement/repository.go`
- `internal/service/settlement/types.go`
- `internal/service/settlement/scheduler.go`
- `internal/command/handler_settlement.go`
- `internal/adapter/message_parser_settlement.go`
- `internal/adapter/formatter_settlement.go`
- `scripts/migrations/038_create_settlement.sql`

### Modified Files (Go)
- `hololive-shared/pkg/domain/command.go` — CommandType 추가
- `internal/adapter/message.go` — 멘션 파싱 경로 추가
- `internal/adapter/message_parser_registry.go` — 파서 등록
- `internal/command/factory.go` — 팩토리 등록
- `internal/command/command.go` — Dependencies에 SettlementRepository 추가
- `internal/app/bootstrap_services.go` — DI 와이어링
- `internal/app/bootstrap_bot.go` — 스케줄러 시작

### Modified Files (Kotlin/Iris)
- `CommandParser.kt` — 멘션 트리거 추가
- `CommandParserTest.kt` — 멘션 테스트 추가

## Out of Scope

- 여러 정산/방 지원 (단일 구독 전용)
- 금액 변경 UI (DB 직접 수정)
- 멤버 추가/제거 UI (DB 직접 수정)
- 카카오 송금 API 연동 (텍스트 기반만)
