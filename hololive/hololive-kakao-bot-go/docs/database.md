# 데이터베이스 아키텍처 및 스키마

## 개요

`hololive-kakao-bot-go` 서비스는 공유 `llm-postgres` 컨테이너 내의 전용 PostgreSQL 데이터베이스 `hololive`를 사용합니다.

### 인프라 구성

| 항목 | 값 |
|------|-----|
| 컨테이너 | `llm-postgres` (docker-compose 서비스명: `postgres`) |
| DB 사용자 | `twentyq_app` (서비스 간 공유) |
| 기본 DB | `twentyq` (game-bot 등 타 봇 사용) |
| 전용 DB | `hololive` (본 봇 전용) |

> **참고**: 애플리케이션은 `twentyq_app` 계정으로 연결하지만, `POSTGRES_DB=hololive` 환경변수로 전용 DB를 명시적으로 선택합니다.

---

## 테이블 상세

### 1. `youtube_stats_history`

YouTube 채널의 통계 스냅샷을 시계열로 저장합니다. 성장 추세 분석 및 그래프 생성에 활용됩니다.

```sql
CREATE TABLE youtube_stats_history (
    time        TIMESTAMPTZ NOT NULL,
    channel_id  VARCHAR(64) NOT NULL,
    member_name VARCHAR(100),
    subscribers BIGINT,
    videos      BIGINT,
    views       BIGINT,
    PRIMARY KEY (time, channel_id)
);
```

**인덱스 전략**:
| 인덱스 | 용도 |
|--------|------|
| `(channel_id, time DESC)` | 특정 채널의 최신 통계 조회 최적화 |
| `(time DESC)` | 시간 범위 쿼리 최적화 |

---

### 2. `alarms`

사용자별 방송 알람 구독 정보의 영속 백업 저장소입니다. 앱 시작 시 Valkey로 일괄 로드됩니다.

```sql
CREATE TABLE alarms (
    id          SERIAL PRIMARY KEY,
    room_id     VARCHAR(64) NOT NULL,
    user_id     VARCHAR(64) NOT NULL,
    channel_id  VARCHAR(64) NOT NULL,
    member_name VARCHAR(200),  -- 알람 추가 시점의 멤버 표시명
    room_name   VARCHAR(200),  -- 방 이름 (캐싱용)
    user_name   VARCHAR(200),  -- 사용자 이름 (캐싱용)
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(room_id, user_id, channel_id)
);
```

**인덱스 전략**:
| 인덱스 | 용도 |
|--------|------|
| `(room_id, user_id)` | 사용자별 알람 조회 |
| `(channel_id)` | 채널별 구독자 조회 |

---

### 3. `youtube_milestones`

구독자 마일스톤 달성 기록을 저장합니다. 중복 알림 방지 및 대시보드 통계에 활용됩니다.

```sql
CREATE TABLE youtube_milestones (
    id          SERIAL PRIMARY KEY,
    channel_id  VARCHAR(24) NOT NULL,
    member_name TEXT NOT NULL,
    type        VARCHAR(20) NOT NULL,  -- 'subscribers'
    value       BIGINT NOT NULL,       -- 예: 500000
    achieved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified    BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(channel_id, type, value)
);
```

**핵심 특징**:
- **멱등성**: `UNIQUE` 제약조건으로 동일 마일스톤 중복 기록 방지 (구독자 수 변동 시에도)
- **알림 상태**: `notified` 플래그로 카카오톡 알림 발송 여부 추적

**인덱스 전략**:
| 인덱스 | 용도 |
|--------|------|
| `(achieved_at DESC)` | "최근 달성 기록" 목록 조회 |
| `(channel_id)` | 채널별 달성 이력 조회 |
| `(notified) WHERE false` | 미발송 알림 효율적 폴링 (부분 인덱스) |
| `UNIQUE(channel_id, type, value)` | 중복 달성 방지 |

---

### 4. `youtube_stats_changes`

통계 변화 감지 기록입니다. 알림 발송 용도로 사용됩니다.

**인덱스 전략**:
| 인덱스 | 용도 |
|--------|------|
| `(detected_at)` | 시간순 조회 |
| `(notified) WHERE false` | 미발송 알림 조회 |

---

### 5. `members`

홀로라이브 멤버 마스터 정보입니다.

**인덱스 전략**:
| 인덱스 | 용도 |
|--------|------|
| `(channel_id)` | YouTube 채널 ID로 조회 |
| `(english_name)` | 영어명 검색 |
| `(slug)` | URL 슬러그 검색 |
| `(status)` | 활동 상태 필터링 |
| `GIN(aliases)` | JSONB 별칭 배열 검색 |
| `GIN(name_search)` | 전문 검색 |

---

## 초기화 스크립트

`scripts/init-db/` 디렉토리의 스크립트는 PostgreSQL 컨테이너 최초 생성 시 자동 실행됩니다.

| 스크립트 | 설명 |
|----------|------|
| `01-create-hololive-db.sh` | `hololive` 데이터베이스 생성 |
| `02-create-youtube-stats-table.sql` | `youtube_stats_history` 테이블 및 인덱스 생성 |
| `03-create-alarms-table.sql` | `alarms` 테이블 생성 |
| `04-create-milestones-table.sql` | `youtube_milestones` 테이블 생성 |

---

## 환경변수 설정

```env
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_DB=hololive
POSTGRES_USER=twentyq_app
POSTGRES_PASSWORD=<비밀번호>
```

> `docker-compose.prod.yml`의 `hololive-bot` 서비스에서 위 환경변수가 설정됩니다.
