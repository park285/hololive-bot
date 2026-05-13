# Config and Environment Contract

## 1. Channel health

### Env

```bash
SCRAPER_CHANNEL_HEALTH_ENABLED=true
SCRAPER_CHANNEL_HEALTH_ENFORCE=false # 권장 추가
SCRAPER_CHANNEL_HEALTH_TTL_SECONDS=86400
SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_BASE_SECONDS=600
SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_MAX_SECONDS=21600
SCRAPER_CHANNEL_HEALTH_TRANSPORT_BASE_SECONDS=120
SCRAPER_CHANNEL_HEALTH_TRANSPORT_MAX_SECONDS=1800
SCRAPER_CHANNEL_HEALTH_TIMEOUT_BASE_SECONDS=120
SCRAPER_CHANNEL_HEALTH_TIMEOUT_MAX_SECONDS=1800
SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_BASE_SECONDS=300
SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_MAX_SECONDS=3600
SCRAPER_CHANNEL_HEALTH_SUCCESS_DECAY_STEPS=1
```

### 권장 default

- Enabled: true
- Enforce: false로 시작 권장
- Parser drift base: 10분
- Parser drift max: 6시간
- Timeout/transport base: 2분
- Timeout/transport max: 30분

## 2. Snapshot

### Env

```bash
SCRAPER_SNAPSHOT_ENABLED=false
SCRAPER_SNAPSHOT_DIR=./artifacts/youtube-scraper
SCRAPER_SNAPSHOT_MAX_BODY_BYTES=524288
SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS=1800
```

### 권장 default

- Enabled: false
- Max body: 512KiB
- Min interval: 30분

## 3. Browser diagnostic

### Env

```bash
SCRAPER_BROWSER_DIAGNOSTIC_ENABLED=false
SCRAPER_BROWSER_DIAGNOSTIC_ENDPOINT=
SCRAPER_BROWSER_DIAGNOSTIC_TIMEOUT_SECONDS=20
SCRAPER_BROWSER_DIAGNOSTIC_MIN_PARSER_DRIFT_FAILURES=3
SCRAPER_BROWSER_DIAGNOSTIC_MAX_PER_HOUR=5
```

### 권장 default

- Enabled: false
- endpoint empty
- max per hour: 5 이하

## 4. Fetcher engine

### Env

```bash
SCRAPER_FETCHER_ENGINE=nethttp
```

허용값:

- `nethttp`
- `goscrapy`
- `browser_snapshot`

주의:

`browser_snapshot`은 normalize 가능하되, 기본 poller path에서 browser를 쓰지 않아야 합니다. 이 값은 diagnostic mode 또는 별도 tool에서만 의미를 갖게 하는 것이 안전합니다.

## 5. Rollback env set

### Snapshot off

```bash
SCRAPER_SNAPSHOT_ENABLED=false
```

### Channel health off

```bash
SCRAPER_CHANNEL_HEALTH_ENABLED=false
```

### Channel health dry-run

```bash
SCRAPER_CHANNEL_HEALTH_ENABLED=true
SCRAPER_CHANNEL_HEALTH_ENFORCE=false
```

### Browser diagnostic off

```bash
SCRAPER_BROWSER_DIAGNOSTIC_ENABLED=false
```

## 6. State key namespace

권장 state keys:

```text
youtube:scraper:channel-health:{source}:{channel_id}
youtube:scraper:snapshot-interval:{operation}:{channel_id}:{stage}:{reason}
youtube:scraper:browser-diagnostic-interval:{channel_id}
```

기존 key와 충돌하지 않아야 합니다.

## 7. Cleanup

### Snapshot files

```bash
find ./artifacts/youtube-scraper -type f -mtime +7 -delete
```

### State keys

운영 cache가 Valkey/Redis라면:

```bash
SCAN 0 MATCH youtube:scraper:channel-health:* COUNT 100
SCAN 0 MATCH youtube:scraper:snapshot-interval:* COUNT 100
```

삭제는 장애 상황에서만 제한적으로 수행합니다.
