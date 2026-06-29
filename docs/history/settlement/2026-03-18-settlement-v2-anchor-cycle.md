# Settlement v2: 18일 앵커 기반 회차 모델 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** settlement-go를 year/month 기반 v1에서 anchor-day 기반 v2 회차 모델로 전면 교체한다.

**Architecture:** 기존 038 마이그레이션은 유지하고 039 추가. Go 코드는 v2 테이블만 사용. handler→service→repository 계층 분리. 회차는 lazy creation + gap-filling으로 보장하고 advisory lock으로 동시성 제어.

**Tech Stack:** Go 1.26, pgx/v5, Valkey, Iris H2C

---

## 설계 확정 사양

### 회차 정의
- 정산 회차는 달력월이 아니라 앵커일(기본 18일) 기준 청구 회차
- 회차 시작: 매월 18일 00:00 KST, 종료: 다음 달 18일 00:00 KST
- 구간 표현: `[period_start_at, period_end_at)`
- 2026-04-01 ~ 2026-04-17은 여전히 2026-03-18 결제분에 속함

### 회차 생성 방식
- lazy creation + gap-filling (`ensureCyclesUpToLocked`)
- 서버 다운타임 후에도 누락 회차를 순차 생성하여 연속성 보장
- v2 rollout 이후 시점부터만 관리 (과거 전 회차 백필 범위 외)

### 멤버 스냅샷
- 회차 생성 시 `settlement_member_terms` 기준 활성 멤버 조회
- `settlement_payments_v2`에 `member_name_snapshot` 포함 row 생성
- 과거 회차의 납부 대상/이름은 변경 불가

### !정산완료 정책
- 미납 1개 → 자동 처리
- 미납 다수 → `MultiplePendingCyclesError` + 회차 지정 요구
- `!정산완료 2026-03-18` 또는 `!정산완료 3/18` (단축) 지원
- 미래 회차 납부 불가

### 알림 정책
- `shouldSendReminder`: `billing_anchor_day - 1`일부터 발송 가능
- Valkey dedup key: `settlement:alarm:{room}:{cycleKey}:{date}`
- 하루 1회 제한, 24시간 TTL

### 동시성
- `pg_advisory_xact_lock(hashtext(roomID))` 방 단위 락
- 전체 회차 생성 + 납부 처리를 단일 트랜잭션 내 수행
- webhook dedup: `settlement_payment_events_v2` (source_type, source_event_id) unique

### 현재 제약
- 한 방 = 한 구독 모델 (다중 구독은 subscription_id 도입 시 확장)

---

## 파일 구조

| 파일 | 역할 | 상태 |
|------|------|------|
| `hololive/hololive-kakao-bot-go/scripts/migrations/039_create_settlement_v2.sql` | v2 스키마 추가 | 신규 |
| `pkg/settlement/types.go` | v2 도메인 모델 | 교체 |
| `pkg/settlement/errors.go` | 도메인 에러 정의 | 신규 |
| `pkg/settlement/cycle_time.go` | 앵커 회차 시간 계산 | 신규 |
| `pkg/settlement/cycle_time_test.go` | 시간 계산 테스트 | 신규 |
| `pkg/settlement/repository.go` | v2 테이블 저장소 (Tx 기반) | 교체 |
| `pkg/settlement/service.go` | 도메인 로직 (회차 보장, 납부 처리) | 신규 |
| `pkg/settlement/scheduler.go` | v2 알람 스케줄러 | 교체 |
| `cmd/settlement/formatter.go` | v2 메시지 포맷 | 교체 |
| `cmd/settlement/handler.go` | v2 webhook 핸들러 | 교체 |
| `cmd/settlement/main.go` | 부트스트랩 (service 계층 추가) | 교체 |

---

## Task 1: v2 마이그레이션 SQL 추가

**Files:**
- Create: `hololive/hololive-kakao-bot-go/scripts/migrations/039_create_settlement_v2.sql`

- [ ] **Step 1: 마이그레이션 파일 작성**

```sql
-- settlement v2: 18일 앵커 기반 회차 모델
-- 기존 038 스키마는 legacy로 유지

CREATE TABLE IF NOT EXISTS settlement_room_configs (
    room_id VARCHAR(64) PRIMARY KEY,
    billing_anchor_day INT NOT NULL DEFAULT 18 CHECK (billing_anchor_day BETWEEN 1 AND 28),
    billing_tz TEXT NOT NULL DEFAULT 'Asia/Seoul',
    total_amount INT NOT NULL DEFAULT 144000,
    per_person INT NOT NULL DEFAULT 36000,
    require_explicit_for_multiple BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO settlement_room_configs (room_id) VALUES
    ('10000000000000001'),
    ('200000000000002'),
    ('10000000000000003')
ON CONFLICT (room_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS settlement_member_terms (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    member_id INT NOT NULL REFERENCES settlement_members(id) ON DELETE CASCADE,
    effective_from_at TIMESTAMPTZ NOT NULL,
    effective_to_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (effective_to_at IS NULL OR effective_to_at > effective_from_at),
    UNIQUE (room_id, member_id, effective_from_at)
);

CREATE INDEX IF NOT EXISTS idx_settlement_member_terms_active
    ON settlement_member_terms (room_id, effective_from_at, effective_to_at);

-- 기존 멤버를 open-ended membership으로 백필
INSERT INTO settlement_member_terms (room_id, member_id, effective_from_at)
SELECT sm.room_id, sm.id, sm.registered_at
FROM settlement_members sm
WHERE NOT EXISTS (
    SELECT 1
    FROM settlement_member_terms smt
    WHERE smt.room_id = sm.room_id
      AND smt.member_id = sm.id
)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS settlement_cycles_v2 (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    cycle_key DATE NOT NULL,
    period_start_at TIMESTAMPTZ NOT NULL,
    period_end_at TIMESTAMPTZ NOT NULL,
    total_amount INT NOT NULL,
    per_person INT NOT NULL,
    billing_anchor_day INT NOT NULL,
    member_count_snapshot INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, cycle_key),
    UNIQUE (room_id, period_start_at),
    CHECK (period_end_at > period_start_at)
);

CREATE INDEX IF NOT EXISTS idx_settlement_cycles_v2_room_start
    ON settlement_cycles_v2 (room_id, period_start_at DESC);

CREATE TABLE IF NOT EXISTS settlement_payments_v2 (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles_v2(id) ON DELETE CASCADE,
    member_id INT NOT NULL REFERENCES settlement_members(id),
    member_name_snapshot VARCHAR(32) NOT NULL,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cycle_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_payments_v2_unpaid
    ON settlement_payments_v2 (cycle_id)
    WHERE paid_at IS NULL;

CREATE TABLE IF NOT EXISTS settlement_payment_events_v2 (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles_v2(id) ON DELETE CASCADE,
    member_id INT NOT NULL REFERENCES settlement_members(id),
    source_type VARCHAR(32) NOT NULL,
    source_event_id VARCHAR(128) NOT NULL,
    paid_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_type, source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_payment_events_v2_cycle_member
    ON settlement_payment_events_v2 (cycle_id, member_id);
```

- [ ] **Step 2: 커밋**

```bash
git add hololive/hololive-kakao-bot-go/scripts/migrations/039_create_settlement_v2.sql
git commit -m "feat(settlement): add v2 anchor-cycle migration (039)"
```

---

## Task 2: 도메인 모델 + 에러 정의

**Files:**
- Replace: `hololive/settlement-go/pkg/settlement/types.go`
- Create: `hololive/settlement-go/pkg/settlement/errors.go`

- [ ] **Step 1: v1 소스 + 빌드 파일 복원**

v1 소스가 staged delete 상태이므로 먼저 git에서 복원합니다:
```bash
cd hololive/settlement-go
git checkout HEAD -- cmd/settlement/ pkg/settlement/ Dockerfile go.mod go.sum
```

- [ ] **Step 2: types.go 교체**

`file.txt` 섹션 2의 코드로 전체 교체:
- `RoomConfig` (방별 설정, anchor_day/tz/amount 포함)
- `Member` (기존 유지)
- `MemberSnapshot` (회차 생성 시 참여 멤버 스냅샷 입력용)
- `Cycle` (v2: CycleKey, PeriodStartAt/EndAt, BillingAnchorDay, MemberCountSnapshot)
- `PaymentStatus` (MemberNameSnapshot 사용)
- `CycleWindow` (시각→회차 범위 계산 결과)
- `PaymentTarget` (!정산완료 대상 후보)
- `PaymentEventRef` (외부 이벤트 dedup 조회 결과)
- `MarkPaidInput` (납부 완료 처리 입력)

- [ ] **Step 3: errors.go 작성**

`file.txt` 섹션 3의 코드 그대로:
- `ErrNotRegisteredMember`, `ErrNoPendingCycle`, `ErrInvalidExplicitCycle`, `ErrFutureCycleNotAllowed`, `ErrCycleNotFoundForMember`, `ErrNoActiveMembers`
- `MultiplePendingCyclesError` (다중 미납 회차)

- [ ] **Step 4: 컴파일 확인**

```bash
cd hololive/settlement-go && go vet ./pkg/settlement/
```
Expected: 컴파일 에러 (repository.go가 아직 v1이므로 타입 불일치). 이 단계에서는 types/errors만 확인.

- [ ] **Step 5: 커밋**

```bash
git add hololive/settlement-go/pkg/settlement/types.go hololive/settlement-go/pkg/settlement/errors.go
git commit -m "feat(settlement): replace domain models with v2 anchor-cycle types"
```

---

## Task 3: 회차 시간 계산 + 테스트

**Files:**
- Create: `hololive/settlement-go/pkg/settlement/cycle_time.go`
- Create: `hololive/settlement-go/pkg/settlement/cycle_time_test.go`

- [ ] **Step 1: cycle_time_test.go 작성**

```go
package settlement

import (
	"testing"
	"time"
)

func TestResolveCycleForMoment(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")

	tests := []struct {
		name     string
		now      time.Time
		wantKey  string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "3/18 당일 → 3/18 회차",
			now:       time.Date(2026, 3, 18, 0, 0, 0, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/1 → 여전히 3/18 회차",
			now:       time.Date(2026, 4, 1, 12, 0, 0, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/17 → 여전히 3/18 회차",
			now:       time.Date(2026, 4, 17, 23, 59, 59, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/18 → 4/18 회차",
			now:       time.Date(2026, 4, 18, 0, 0, 0, 0, kst),
			wantKey:   "2026-04-18",
			wantStart: "2026-04-18",
			wantEnd:   "2026-05-18",
		},
		{
			name:      "3/17 → 2/18 회차",
			now:       time.Date(2026, 3, 17, 23, 59, 59, 0, kst),
			wantKey:   "2026-02-18",
			wantStart: "2026-02-18",
			wantEnd:   "2026-03-18",
		},
		{
			name:      "1/1 → 12/18 회차 (연말 경계)",
			now:       time.Date(2026, 1, 1, 0, 0, 0, 0, kst),
			wantKey:   "2025-12-18",
			wantStart: "2025-12-18",
			wantEnd:   "2026-01-18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win, err := ResolveCycleForMoment(cfg, tt.now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if win.CycleKey != tt.wantKey {
				t.Errorf("CycleKey = %s, want %s", win.CycleKey, tt.wantKey)
			}
			startLocal := win.StartAt.In(kst).Format("2006-01-02")
			if startLocal != tt.wantStart {
				t.Errorf("StartAt(KST) = %s, want %s", startLocal, tt.wantStart)
			}
			endLocal := win.EndAt.In(kst).Format("2006-01-02")
			if endLocal != tt.wantEnd {
				t.Errorf("EndAt(KST) = %s, want %s", endLocal, tt.wantEnd)
			}
		})
	}
}

func TestNormalizeExplicitCycleKey(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, kst)

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"full date", "2026-03-18", "2026-03-18", nil},
		{"short M/D", "3/18", "2026-03-18", nil},
		{"wrong day", "3/15", "", ErrInvalidExplicitCycle},
		{"wrong format", "abc", "", ErrInvalidExplicitCycle},
		{"empty", "", "", nil},
		{"year rollover 12/18 when now is 2026-01", "12/18", "2025-12-18", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeExplicitCycleKey(cfg, tt.raw, now)
			if tt.wantErr != nil {
				if err == nil || err != tt.wantErr {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNextCycleStart(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")

	start := time.Date(2026, 3, 18, 0, 0, 0, 0, kst).UTC()
	next, err := NextCycleStart(cfg, start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantNext := time.Date(2026, 4, 18, 0, 0, 0, 0, kst).UTC()
	if !next.Equal(wantNext) {
		t.Errorf("next = %v, want %v", next, wantNext)
	}
}
```

- [ ] **Step 2: 테스트 실행 - 실패 확인**

```bash
cd hololive/settlement-go && go test -v -run TestResolveCycleForMoment ./pkg/settlement/
```
Expected: FAIL (cycle_time.go 미존재)

- [ ] **Step 3: cycle_time.go 작성**

`file.txt` 섹션 4의 코드 그대로:
- `loadLocation` (timezone 로드)
- `ResolveCycleForMoment` (시각→회차 계산)
- `NextCycleStart` (다음 회차 시작 계산)
- `CycleKeyFromStart` (UTC→cycle_key)
- `NormalizeExplicitCycleKey` (사용자 입력 정규화: YYYY-MM-DD, M/D)

- [ ] **Step 4: 테스트 실행 - 통과 확인**

```bash
cd hololive/settlement-go && go test -v -run "TestResolveCycleForMoment|TestNormalizeExplicitCycleKey|TestNextCycleStart" ./pkg/settlement/
```
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add hololive/settlement-go/pkg/settlement/cycle_time.go hololive/settlement-go/pkg/settlement/cycle_time_test.go
git commit -m "feat(settlement): add anchor-cycle time calculation with tests"
```

---

## Task 4: Repository 교체 (v2 테이블, Tx 기반)

**Files:**
- Replace: `hololive/settlement-go/pkg/settlement/repository.go`

- [ ] **Step 1: repository.go 교체**

`file.txt` 섹션 5의 코드로 전체 교체. 핵심 변경:
- 모든 메서드 `*Tx` 접미사 (트랜잭션 범위 내 동작)
- `WithTx` (트랜잭션 래퍼)
- `LockRoomTx` (`pg_advisory_xact_lock(hashtext(roomID))`)
- `GetRoomConfigTx` (방 설정 upsert + 조회)
- `EnsureMemberTermsSeededTx` (기존 멤버 → membership term 백필)
- `ListActiveMembersAtTx` (시점 기준 활성 멤버 조회)
- `InsertCycleTx` / `FindCycleByKeyTx` / `GetLatestCycleTx` / `GetCycleByIDTx`
- `InsertCyclePaymentRowTx` / `GetPaymentStatusesTx` / `GetUnpaidMemberNamesTx`
- `FindPaymentTargetByCycleKeyTx` / `ListPendingPaymentTargetsUpToTx` (`FOR UPDATE OF sp`)
- `MarkPaidTx` (`COALESCE(paid_at, $3)` 멱등성)
- `FindPaymentEventBySourceTx` / `InsertPaymentEventTx` (webhook dedup)

- [ ] **Step 2: 컴파일 확인**

```bash
cd hololive/settlement-go && go vet ./pkg/settlement/
```
Expected: 에러 없음 (타입은 Task 2에서 교체 완료)

- [ ] **Step 3: 커밋**

```bash
git add hololive/settlement-go/pkg/settlement/repository.go
git commit -m "feat(settlement): replace repository with v2 tx-based implementation"
```

---

## Task 5: Service 계층 추가

**Files:**
- Create: `hololive/settlement-go/pkg/settlement/service.go`

- [ ] **Step 1: service.go 작성**

`file.txt` 섹션 6의 코드로. 핵심 메서드:
- `GetStatus` (현재 회차 보장 + 현황 반환)
- `MarkPaid` (!정산완료: dedup → 회차 보장 → 멤버 조회 → target 해석 → 납부 기록)
- `GetReminderPayload` (알람 대상 조회)
- `shouldSendReminder` (billing_anchor_day - 1일부터 발송)
- `ensureCyclesUpToLocked` (lazy creation + gap-filling)
- `createCycleSnapshotLocked` (회차 + 멤버 스냅샷 생성)
- `resolvePayTargetLocked` (납부 대상 해석: 명시/자동/다중 미납 분기)

모든 public 메서드는 `WithTx` + `LockRoomTx` 패턴 사용.

- [ ] **Step 2: 컴파일 확인**

```bash
cd hololive/settlement-go && go vet ./pkg/settlement/
```
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
git add hololive/settlement-go/pkg/settlement/service.go
git commit -m "feat(settlement): add service layer with cycle management and payment logic"
```

---

## Task 6: Scheduler 교체

**Files:**
- Replace: `hololive/settlement-go/pkg/settlement/scheduler.go`

- [ ] **Step 1: scheduler.go 교체**

`file.txt` 섹션 7의 코드로. v1 대비 변경:
- `repo *Repository` → `svc *Service` 의존
- `FormatAlarmFunc` 시그니처 변경: `func(cycle *Cycle, unpaidNames []string) string`
- `check()`: `svc.GetReminderPayload()` 호출 → cycle/unpaid 반환 → dedup → 발송
- dedup key: `settlement:alarm:{room}:{cycleKey}:{date}` (v1의 year:month:day → v2의 cycleKey:date)

- [ ] **Step 2: 컴파일 확인**

```bash
cd hololive/settlement-go && go vet ./pkg/settlement/
```
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
git add hololive/settlement-go/pkg/settlement/scheduler.go
git commit -m "feat(settlement): replace scheduler with v2 service-driven implementation"
```

---

## Task 7: Formatter + Handler + Main 교체

**Files:**
- Replace: `hololive/settlement-go/cmd/settlement/formatter.go`
- Replace: `hololive/settlement-go/cmd/settlement/handler.go`
- Replace: `hololive/settlement-go/cmd/settlement/main.go`

- [ ] **Step 1: formatter.go 교체**

`file.txt` 섹션 8의 코드로. 변경:
- `formatStatus`: 이용기간/갱신일 표시, `MemberNameSnapshot` 사용
- `formatAlarm`: 시그니처 변경 `func(cycle *Cycle, unpaidNames []string) string`
- `cycleLabel` / `cycleRange` 헬퍼 추가

- [ ] **Step 2: handler.go 교체**

`file.txt` 섹션 9의 코드로. 변경:
- `botHandler.repo` → `botHandler.svc` 의존
- `parseAction` → `parseCommand` (ExplicitCycleKey 추출 포함)
- `handlePaid`: `MarkPaidInput` 구성 (ExplicitCycleKey, SourceType, SourceEventID)
- 에러 분기: `MultiplePendingCyclesError`, `ErrFutureCycleNotAllowed`, `ErrCycleNotFoundForMember` 등

- [ ] **Step 3: main.go 교체**

`file.txt` 섹션 10의 코드로. 변경:
- `settlement.NewService(repo)` 추가
- `botHandler{svc: svc, ...}` (repo 직접 참조 제거)
- `NewScheduler(svc, ...)` (repo → svc)

- [ ] **Step 4: 컴파일 확인**

```bash
cd hololive/settlement-go && go vet ./...
```
Expected: PASS

- [ ] **Step 5: 기존 v1 테스트 삭제**

기존 `main_test.go`, `server_test.go`는 v1 타입(`Year`, `Month`, `DueDay`)을 참조하므로 삭제합니다.
v2 API surface와 호환되지 않으므로 복구 불필요:
```bash
rm -f hololive/settlement-go/cmd/settlement/main_test.go hololive/settlement-go/cmd/settlement/server_test.go
```

- [ ] **Step 6: 커밋**

```bash
git add hololive/settlement-go/cmd/settlement/
git commit -m "feat(settlement): replace handler/formatter/main with v2 service-driven implementation"
```

---

## Task 8: 빌드 검증 + 통합 커밋

**Files:** (전체)

- [ ] **Step 1: 전체 빌드 확인**

```bash
cd hololive/settlement-go && go build ./...
```
Expected: PASS

- [ ] **Step 2: 전체 테스트 실행**

```bash
cd hololive/settlement-go && go test -v ./...
```
Expected: PASS (cycle_time_test.go 통과)

- [ ] **Step 3: lint 확인**

```bash
cd hololive/settlement-go && golangci-lint run ./... 2>/dev/null || go vet ./...
```

- [ ] **Step 4: 불필요한 바이너리 정리**

settlement-go 루트의 기존 바이너리 파일 제거 (git tracked가 아니므로):
```bash
rm -f hololive/settlement-go/settlement
```

---

## v1 → v2 변경 요약

| 구분 | v1 | v2 |
|------|----|----|
| 회차 정의 | `year/month` | `cycle_key=YYYY-MM-DD` (앵커일 기반) |
| 회차 테이블 | `settlement_cycles` | `settlement_cycles_v2` |
| 납부 테이블 | `settlement_payments` | `settlement_payments_v2` (member_name_snapshot) |
| 회차 생성 | ON CONFLICT 멱등 | lazy + gap-filling |
| 멤버 스냅샷 | 없음 (현재 멤버 전체) | `settlement_member_terms` 기준 스냅샷 |
| 동시성 | 없음 | `pg_advisory_xact_lock` |
| webhook dedup | 없음 | `settlement_payment_events_v2` |
| 계층 구조 | handler→repo | handler→service→repo |
| !정산완료 | 현재 month 고정 | 단일/다중 분기 + 회차 지정 |
| 알림 | day≥17 고정 | anchor_day-1 부터 |
