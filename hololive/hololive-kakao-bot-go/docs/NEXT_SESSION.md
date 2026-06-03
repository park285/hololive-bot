# 다음 세션 작업 가이드

> 마지막 업데이트: 2026-01-22

## 현재 상태

### 완료된 작업

| 작업 | 버전 | 상태 |
|------|------|------|
| 알람 타입 시스템 구현 | v2.0.21 | 배포됨 |
| DB 마이그레이션 (alarm_types, notification_templates) | 010 | 완료 |
| 기존 사용자 데이터 마이그레이션 (9개 레코드) | - | 완료 |
| TemplateRenderer (DB 기반 템플릿) | - | 구현됨 |
| **YouTube Poller 테이블 마이그레이션** | 011 | 완료 |
| **Outbox Dispatcher 통합 테스트 파일 생성** | - | 완료 |

### 생성된 테이블 (011 마이그레이션)

| 테이블 | 레코드 수 | 설명 |
|--------|-----------|------|
| `youtube_videos` | 461+ | 영상/쇼츠 메타데이터 |
| `youtube_notification_outbox` | 0 | 알림 발송 큐 (초기 동기화 중) |
| `youtube_content_watermarks` | 47 | 중복 알림 방지 워터마크 |
| `youtube_community_posts` | 0 | 커뮤니티 포스트 |
| `youtube_live_sessions` | 0 | 라이브 세션 상태 |
| `youtube_live_viewer_samples` | 0 | 시청자 샘플 데이터 |
| `youtube_stream_stats` | 0 | 방송 집계 통계 |
| `youtube_channel_stats_snapshots` | 0 | 채널 통계 스냅샷 |
| `youtube_channel_profiles` | 0 | 채널 프로필 (아바타/배너) |

### 배포된 기능

**알람 타입 선택 명령어**:
```
!알람 추가 <멤버>           → 전체 (방송+커뮤니티+쇼츠)
!알람 추가 <멤버> 방송      → 방송만
!알람 추가 <멤버> 커뮤니티  → 커뮤니티만
!알람 추가 <멤버> 쇼츠      → 쇼츠만
!알람 추가 <멤버> 전체      → 전체

!알람 제거 <멤버> [타입]    → 동일 형식
```

**지원 키워드**:
| 타입 | 키워드 |
|------|--------|
| 방송 | `방송`, `라이브`, `live` |
| 커뮤니티 | `커뮤니티`, `community` |
| 쇼츠 | `쇼츠`, `shorts` |
| 전체 | `전체`, `all` |

---

## 남은 작업 (우선순위순)

### 1. Outbox Dispatcher 통합 테스트 실행

테스트 파일이 생성되었으나, Docker 환경 내에서 실행해야 합니다:

```bash
# Docker 컨테이너 내에서 테스트 실행
docker exec -it hololive-kakao-bot-go sh -c \
  "INTEGRATION_TEST=true go test -v ./internal/service/youtube/outbox/..."

# 또는 CI/CD 환경에서 실행
INTEGRATION_TEST=true \
TEST_DATABASE_URL="host=llm-postgres port=5432 user=twentyq_app password=... dbname=hololive sslmode=require" \
TEST_VALKEY_HOST=valkey-cache \
go test -v ./internal/service/youtube/outbox/...
```

**테스트 케이스**:
- `TestDispatcher_ProcessOnce_Success` - 성공 경로
- `TestDispatcher_ProcessOnce_Retry` - 실패/재시도 경로
- `TestDispatcher_NoSubscribers_MarkedAsSent` - 구독자 없음 처리

### 2. 알람 목록에 타입 표시 (선택)

현재 `!알람 목록`에서 구독 타입이 표시되지 않음.
- `internal/command/alarm.go` → `handleList()`
- `internal/adapter/message.go` → `FormatAlarmList()` 수정

---

## 아키텍처 요약

```
┌─────────────────────────────────────────────────────────────────┐
│                        알람 시스템 흐름                           │
└─────────────────────────────────────────────────────────────────┘

User → !알람 추가 <멤버> [타입]
         │
         ▼
┌─────────────────┐
│  MessageAdapter │ → extractMemberAndType()로 멤버/타입 분리
│  (message.go)   │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  AlarmCommand   │ → parseAlarmTypes()로 domain.AlarmTypes 변환
│  (alarm.go)     │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  AlarmService   │ → AddAlarm(ctx, roomID, userID, channelID, ..., alarmTypes)
│(alarm_service.go)│
└────────┬────────┘
         │
         ├──────────────────────────────────────┐
         ▼                                      ▼
┌─────────────────┐                  ┌─────────────────────┐
│  Valkey Cache   │                  │  PostgreSQL (DB)    │
│                 │                  │                     │
│  타입별 키:      │                  │  alarms 테이블       │
│  - alarm:channel_subscribers:{id}  │  - alarm_types 컬럼  │
│  - alarm:channel_subscribers:      │    ARRAY['LIVE',     │
│    COMMUNITY:{id}                  │    'COMMUNITY',      │
│  - alarm:channel_subscribers:      │    'SHORTS']         │
│    SHORTS:{id}                     │                     │
└─────────────────┘                  └─────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                     알림 발송 흐름 (완료)                       │
└─────────────────────────────────────────────────────────────────┘

YouTube API / Scraper
         │
         ▼
┌──────────────────────────────┐
│  youtube-producer pollers     │ → 새 쇼츠/커뮤니티 감지
│  (youtube-producer runtime)   │
└──────────────┬───────────────┘
         │
         ▼
┌─────────────────┐
│  youtube_videos │ ← 테이블 생성 완료 (461+ 레코드)
│  (DB Table)     │
└────────┬────────┘
         │
         ▼
┌─────────────────────────┐
│ youtube_notification_   │ ← 테이블 생성 완료
│ outbox (DB Table)       │
└────────┬────────────────┘
         │
         ▼
┌─────────────────┐
│ OutboxDispatcher│ → kind.ToAlarmType()로 타입 결정
│ (dispatcher.go) │    GetChannelSubscribersByType()로 구독자 조회
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│TemplateRenderer │ → DB 템플릿 or 기본 템플릿
│ (renderer.go)   │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Iris → Kakao  │ → 메시지 발송
└─────────────────┘
```

---

## 수정된 파일 목록

### 신규 생성 (이번 세션)
| 파일 | 설명 |
|------|------|
| `scripts/migrations/011-create-youtube-content-tables.sql` | YouTube 콘텐츠 테이블 마이그레이션 |
| `internal/service/youtube/outbox/dispatcher_integration_test.go` | Dispatcher 통합 테스트 |

### 수정됨 (이번 세션)
| 파일 | 변경 내용 |
|------|-----------|
| `internal/service/youtube/outbox/dispatcher.go` | ProcessOnceForTest() 테스트용 메서드 추가 |

### 기존 파일
| 파일 | 설명 |
|------|------|
| `internal/domain/notification_template.go` | NotificationTemplate 모델 + TemplateKey 상수 |
| `internal/service/template/renderer.go` | DB 기반 템플릿 렌더러 |
| `scripts/migrations/010-add-alarm-types-and-templates.sql` | 마이그레이션 |

---

## 다음 세션 시작 명령어

```bash
# 1. 현재 상태 확인
docker logs hololive-kakao-bot-go --tail 30

# 2. DB 테이블 데이터 확인
docker exec llm-postgres psql -U twentyq_app -d hololive -c \
  "SELECT count(*) as cnt, 'youtube_videos' as tbl FROM youtube_videos 
   UNION ALL SELECT count(*), 'youtube_notification_outbox' FROM youtube_notification_outbox 
   UNION ALL SELECT count(*), 'youtube_content_watermarks' FROM youtube_content_watermarks;"

# 3. 알람 데이터 확인
docker exec llm-postgres psql -U twentyq_app -d hololive -c \
  "SELECT id, channel_id, alarm_types FROM alarms LIMIT 10;"

# 4. 에러 로그 확인
docker logs hololive-kakao-bot-go --since 5m 2>&1 | grep -E "ERR|WRN|Failed"
```

---

## 연락처

질문이 있으면 이 문서와 함께 새 세션을 시작하세요.
