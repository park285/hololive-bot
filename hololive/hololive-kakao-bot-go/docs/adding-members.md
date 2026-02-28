# VTuber 멤버 추가 가이드

> 새로운 VTuber 멤버를 시스템에 추가하는 방법을 설명합니다.

## 개요

멤버 추가 방식은 **그룹(org)**에 따라 다릅니다:

| 그룹 | 추가 방식 | sync_source |
|------|----------|-------------|
| Hololive | Holodex API 자동 동기화 | `holodex` |
| Nijisanji | Holodex API 자동 동기화 | `holodex` |
| VSPO | Holodex API 자동 동기화 | `holodex` |
| **Indie** | **수동 등록 필요** | `manual` |

---

## 1. Indie(개인세) VTuber 추가

개인세 VTuber는 Holodex에 등록되어 있지 않으므로 **수동으로 추가**해야 합니다.

### 1.1 필요한 정보 수집

| 필드 | 설명 | 예시 | 필수 |
|------|------|------|------|
| `channel_id` | YouTube 채널 ID | `UCrV1Hf5r8P148idjoSfrGEQ` | O |
| `english_name` | 영어 이름 | `Yuuki Sakuna` | O |
| `japanese_name` | 일본어 이름 | `結城さくな` | O |
| `korean_name` | 한국어 이름 | `유우키 사쿠나` | O |
| `slug` | URL용 식별자 | `yuuki-sakuna` | O |
| `aliases` | 별칭 (JSON) | `{"ko":["사쿠나"],"ja":["さくな"]}` | X |

### 1.2 DB에 멤버 추가

```sql
INSERT INTO members (
    slug, 
    channel_id, 
    english_name, 
    japanese_name, 
    korean_name, 
    org, 
    sync_source, 
    status, 
    is_graduated, 
    aliases
)
VALUES (
    'new-vtuber-slug',
    'UC채널ID여기에',
    'English Name',
    '日本語名前',
    '한국어 이름',
    'Indie',
    'manual',
    'active',
    false,
    '{"ko":["별칭1","별칭2"],"ja":["エイリアス"]}'
)
ON CONFLICT DO NOTHING;
```

### 1.3 IndieChannelIDs 상수 업데이트 (중요!)

**파일**: `internal/constants/constants.go`

Indie 멤버의 라이브 스트림을 조회하려면 채널 ID를 상수에 추가해야 합니다:

```go
// internal/constants/constants.go

// IndieChannelIDs는 개인세 VTuber 채널 ID 목록입니다.
// Holodex API에서 org=Indie가 지원되지 않아 /users/live API로 직접 조회합니다.
var IndieChannelIDs = []string{
    "UCrV1Hf5r8P148idjoSfrGEQ", // 結城さくな (Yuuki Sakuna)
    "UCxsZ6NCzjU_t4YSxQLBcM5A", // 사메코 사바 (Sameko Saba)
    // 새 멤버 추가:
    "UC새채널ID여기에",          // 새 멤버 이름
}
```

### 1.4 배포

```bash
# 1. DB 마이그레이션 (또는 직접 INSERT)
psql -d <database> -f scripts/migrations/0XX-add-new-member.sql

# 2. 애플리케이션 재빌드 (상수 변경 시)
./build-all.sh hololive-bot

# 3. 캐시 갱신 (앱 재시작 시 자동)
```

---

## 2. members 테이블 스키마

```sql
CREATE TABLE members (
    id              SERIAL PRIMARY KEY,
    slug            VARCHAR(100) UNIQUE NOT NULL,
    channel_id      VARCHAR(50) UNIQUE NOT NULL,
    english_name    VARCHAR(100) NOT NULL,
    japanese_name   VARCHAR(100),
    korean_name     VARCHAR(100),
    org             VARCHAR(50) NOT NULL,      -- Hololive, Nijisanji, VSPO, Indie
    suborg          VARCHAR(100),              -- EN, JP, KR, ID 등
    sync_source     VARCHAR(20) NOT NULL,      -- holodex, manual
    status          VARCHAR(20) DEFAULT 'active',
    is_graduated    BOOLEAN DEFAULT false,
    aliases         JSONB,                     -- {"ko":[], "ja":[], "en":[]}
    photo           TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

### 주요 필드 설명

| 필드 | 설명 |
|------|------|
| `org` | 소속 그룹. `Hololive`, `Nijisanji`, `VSPO`, `Indie` 중 하나 |
| `suborg` | 세부 그룹. 예: `EN`, `JP`, `KR`, `ID`, `HoloX` 등 |
| `sync_source` | 동기화 출처. `holodex` (자동), `manual` (수동) |
| `aliases` | 검색용 별칭. JSON 형식으로 언어별 별칭 저장 |

---

## 3. 관련 코드 위치

| 목적 | 파일 | 설명 |
|------|------|------|
| 상수 정의 | `internal/constants/constants.go` | `IndieChannelIDs`, `SyncTargetOrgs` |
| DB 모델 | `internal/service/member/repository.go` | GORM Model 구조체 |
| 도메인 모델 | `internal/domain/member.go` | Member 구조체 |
| 캐시 초기화 | `internal/app/providers.go` | `ProvideMemberCache()` |
| Holodex 조회 | `internal/service/holodex/service.go` | `fetchIndieStreams()` |

---

## 4. 검증 방법

### 4.1 DB 확인

```sql
-- 멤버 존재 확인
SELECT id, english_name, org, sync_source 
FROM members 
WHERE channel_id = 'UC새채널ID';

-- Indie 멤버 목록
SELECT english_name, korean_name, channel_id 
FROM members 
WHERE org = 'Indie';
```

### 4.2 캐시 확인

```bash
# Valkey 캐시 확인
docker exec valkey-cache valkey-cli HGETALL hololive:members | grep "새멤버이름"
```

### 4.3 라이브 스트림 조회 테스트

```bash
# 봇 명령어로 테스트
!라이브
# → [개인세] 새멤버 표시 확인

!알람 추가 새멤버이름
# → 알람 등록 성공 확인
```

---

## 5. 체크리스트

새 Indie 멤버 추가 시:

- [ ] YouTube 채널 ID 확인
- [ ] `members` 테이블에 INSERT
- [ ] `IndieChannelIDs` 상수에 채널 ID 추가
- [ ] 애플리케이션 재빌드 및 배포
- [ ] `!라이브` 명령어로 스트림 조회 테스트
- [ ] `!알람 추가` 명령어로 알람 등록 테스트

---

## 6. 예시: 새 멤버 추가 전체 과정

### 6.1 마이그레이션 파일 생성

```bash
# scripts/migrations/017-add-new-indie-member.sql
```

```sql
-- 017-add-new-indie-member.sql
-- 새 Indie VTuber 추가: 예시 이름

INSERT INTO members (
    slug, channel_id, english_name, japanese_name, korean_name,
    org, sync_source, status, is_graduated, aliases
)
VALUES (
    'example-vtuber',
    'UCxxxxxxxxxxxxxxxxxx',
    'Example VTuber',
    '例のVTuber',
    '예시 브이튜버',
    'Indie',
    'manual',
    'active',
    false,
    '{"ko":["예시","브튜버"],"ja":["れい"]}'
)
ON CONFLICT DO NOTHING;
```

### 6.2 상수 업데이트

```go
// internal/constants/constants.go

var IndieChannelIDs = []string{
    "UCrV1Hf5r8P148idjoSfrGEQ", // 結城さくな
    "UCxsZ6NCzjU_t4YSxQLBcM5A", // 사메코 사바
    "UCxxxxxxxxxxxxxxxxxx",     // 예시 브이튜버 (NEW)
}
```

### 6.3 배포

```bash
# DB 마이그레이션
psql -d hololive_bot -f scripts/migrations/017-add-new-indie-member.sql

# 앱 재빌드
./build-all.sh hololive-bot
```

---

## 7. 주의사항

1. **sync_source='manual' 필수**: Indie 멤버는 반드시 `sync_source='manual'`로 설정해야 Holodex 동기화에서 덮어쓰기되지 않습니다.

2. **IndieChannelIDs 누락 시**: 채널 ID가 상수에 없으면 라이브 스트림이 조회되지 않습니다.

3. **캐시 갱신**: 앱 재시작 시 자동으로 캐시가 갱신됩니다. 수동 갱신이 필요하면:
   ```bash
   docker exec valkey-cache valkey-cli DEL hololive:members
   ```

4. **동명이인**: 다른 그룹에 같은 이름의 멤버가 있으면 `!알람` 명령어에서 선택 리스트가 표시됩니다.

---

**Last Updated**: 2026-01-27
