# Milestone API Documentation

## Overview

마일스톤 API는 홀로라이브 멤버들의 YouTube 구독자 마일스톤 달성 현황을 조회하는 기능을 제공합니다.

## Authentication

모든 마일스톤 API는 Admin 인증이 필요합니다. 요청 시 유효한 `admin_session` 쿠키가 포함되어야 합니다.

> **보안 참고**: 이 API는 내부 Admin 대시보드(웹 프론트엔드)와 긴밀히 결합된 내부 API입니다. 쿠키 기반 인증을 사용하며, CSRF 보호는 `SameSite=Strict` 쿠키 정책과 Cloudflare Access를 통해 제공됩니다.

---

## Endpoints

### 1. 달성된 마일스톤 목록 조회

**GET** `/admin/api/holo/milestones`

달성된 마일스톤 목록을 최신순으로 반환합니다. 페이지네이션 및 필터링을 지원합니다.

#### Query Parameters

| Parameter    | Type   | Default | Description                    |
|--------------|--------|---------|--------------------------------|
| `limit`      | number | 50      | 반환할 최대 항목 수 (1-100)     |
| `offset`     | number | 0       | 건너뛸 항목 수 (페이지네이션)    |
| `channelId`  | string | -       | 특정 채널 ID로 필터링           |
| `memberName` | string | -       | 멤버 이름으로 검색 (부분 일치)   |

#### Response

```json
{
  "status": "ok",
  "milestones": [
    {
      "channelId": "UC1DCedRgGHBdm81E1llLhOQ",
      "memberName": "Pekora Ch. 兎田ぺこら",
      "type": "subscribers",
      "value": 3000000,
      "achievedAt": "2024-12-15T10:30:00Z",
      "notified": true
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

#### Response Fields

| Field       | Type      | Description                 |
|-------------|-----------|-----------------------------|
| `channelId` | string    | YouTube 채널 ID             |
| `memberName`| string    | 멤버 이름                   |
| `type`      | string    | 마일스톤 유형 (`subscribers`) |
| `value`     | number    | 달성한 마일스톤 값          |
| `achievedAt`| timestamp | 시스템 기록 시각 (UTC)      |
| `notified`  | boolean   | 알림 발송 여부              |
| `total`     | number    | 전체 항목 수 (페이지네이션용)|
| `limit`     | number    | 요청된 limit 값             |
| `offset`    | number    | 요청된 offset 값            |

> **참고**: `achievedAt`은 **시스템 기록 시각**입니다. 실제 YouTube 달성 시각과 최대 1~12시간 오차가 있을 수 있습니다 (수집 주기 의존).

---

### 2. 마일스톤 직전 멤버 조회

**GET** `/admin/api/holo/milestones/near`

마일스톤 달성이 임박한 멤버 목록을 반환합니다.

#### Query Parameters

| Parameter   | Type   | Default | Description                               |
|-------------|--------|---------|-------------------------------------------|
| `threshold` | number | 0.95    | 진행률 임계값 (0.0-1.0, 기본값: 95%)        |

> **참고**: 기본값 `0.95`는 백그라운드 워커(`MilestoneThresholdRatio`)와 동일한 값입니다. API 조회 시 다른 임계값을 지정할 수 있습니다.

#### Response

```json
{
  "status": "ok",
  "members": [
    {
      "channelId": "UCL_qhgtOy0dy1Agp8vkySQg",
      "memberName": "Mori Calliope",
      "currentSubs": 2450000,
      "nextMilestone": 2500000,
      "remaining": 50000,
      "progressPct": 98.0
    }
  ],
  "count": 1,
  "threshold": 0.95
}
```

#### Response Fields

| Field          | Type   | Description                          |
|----------------|--------|--------------------------------------|
| `channelId`    | string | YouTube 채널 ID                      |
| `memberName`   | string | 멤버 이름                            |
| `currentSubs`  | number | 현재 구독자 수                       |
| `nextMilestone`| number | 다음 마일스톤 값                     |
| `remaining`    | number | 마일스톤까지 남은 구독자 수          |
| `progressPct`  | number | 진행률 (%) - 0-100 범위              |

---

### 3. 마일스톤 통계 요약

**GET** `/admin/api/holo/milestones/stats`

마일스톤 관련 전체 통계를 반환합니다.

#### Response

```json
{
  "status": "ok",
  "stats": {
    "totalAchieved": 150,
    "totalNearMilestone": 5,
    "recentAchievements": 3,
    "notNotifiedCount": 0
  }
}
```

#### Response Fields

| Field                | Type   | Description                          |
|----------------------|--------|--------------------------------------|
| `totalAchieved`      | number | 총 달성된 마일스톤 수                |
| `totalNearMilestone` | number | 현재 마일스톤 직전 멤버 수 (95% 이상)|
| `recentAchievements` | number | 최근 30일 내 달성된 마일스톤 수      |
| `notNotifiedCount`   | number | 알림이 발송되지 않은 마일스톤 수     |

---

## Milestone Values

시스템에서 추적하는 마일스톤 값 (단일 정의: `youtube.SubscriberMilestones`):

| Value     | 한국어 표기 |
|-----------|------------|
| 100,000   | 10만       |
| 250,000   | 25만       |
| 500,000   | 50만       |
| 750,000   | 75만       |
| 1,000,000 | 100만      |
| 1,500,000 | 150만      |
| 2,000,000 | 200만      |
| 2,500,000 | 250만      |
| 3,000,000 | 300만      |
| 4,000,000 | 400만      |
| 5,000,000 | 500만      |

> **확장성 참고**: 새로운 마일스톤 값(예: 600만, 1000만, 중간 단위 125만 등) 추가 시 `internal/service/youtube/scheduler.go`의 `SubscriberMilestones` 변수를 수정하고 재배포해야 합니다. 현재는 코드에 하드코딩되어 있으며, 향후 설정 파일 또는 DB 기반 관리로 확장 가능합니다.

---

## Error Responses

### 400 Bad Request

```json
{
  "error": "Invalid parameter"
}
```

잘못된 파라미터 형식 (예: limit에 문자열 입력, 범위 초과).

### 401 Unauthorized

```json
{
  "error": "Unauthorized"
}
```

인증되지 않은 요청.

### 500 Internal Server Error

```json
{
  "error": "Failed to get milestones"
}
```

데이터베이스 조회 중 오류.

### 503 Service Unavailable

```json
{
  "error": "Stats repository not available"
}
```

YouTube stats repository가 초기화되지 않음 (환경 설정 문제).

---

## Implementation Notes

### Dual-Layer Monitoring

마일스톤 모니터링은 두 가지 계층으로 동작합니다:

| 계층 | API | 주기 | 대상 | Threshold |
|------|-----|------|------|-----------|
| Standard | YouTube Data API | 12시간 | 전체 멤버 | - |
| Fast-Track | Holodex API | 1시간 | 마일스톤 직전 멤버 | **95%** |

- **Standard Layer**: YouTube API의 일일 할당량(Quota)을 고려하여 12시간 간격으로 전체 멤버의 구독자 데이터를 수집합니다.
- **Fast-Track Layer**: Holodex API를 사용하여 **95% 이상 진행된 멤버**만 1시간 간격으로 빠르게 체크합니다. 이를 통해 마일스톤 달성 시 빠른 알림이 가능합니다.

> **API vs 워커 Threshold**: API 기본값(`0.95`)과 백그라운드 워커(`MilestoneThresholdRatio = 0.95`)는 동일합니다.

### Graduated Member Filtering

졸업한 멤버 (`is_graduated = true`)는 마일스톤 추적에서 자동으로 제외됩니다.

### Re-achievement Prevention

한 번 달성된 마일스톤은 `youtube_milestones` 테이블에 기록되며, 구독자 수가 일시적으로 감소했다가 다시 증가해도 중복 알림이 발생하지 않습니다.

### Single Source of Truth

마일스톤 값 목록(`SubscriberMilestones`), 임계값(`MilestoneThresholdRatio`)은 `internal/service/youtube/scheduler.go`에 **단일 정의**되어 있으며, API 핸들러에서도 이를 참조합니다.

---

## Database Schema

### youtube_milestones

```sql
CREATE TABLE youtube_milestones (
    id SERIAL PRIMARY KEY,
    channel_id VARCHAR(24) NOT NULL,
    member_name TEXT NOT NULL,
    type VARCHAR(20) NOT NULL,  -- 'subscribers'
    value BIGINT NOT NULL,
    achieved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(channel_id, type, value)
);
```

> **`achieved_at` 참고**: 기본값 `NOW()`는 **시스템 기록 시각**입니다. 12시간/1시간 수집 주기 사이에 실제 달성이 일어나므로, 실제 YouTube 달성 시각과 최대 1-12시간 오차가 있습니다.
