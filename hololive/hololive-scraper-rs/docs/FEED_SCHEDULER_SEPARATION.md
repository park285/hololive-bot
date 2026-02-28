# Feed Scheduler Separation (스케줄러 수준 도메인 분리)

> 단일 `ScraperScheduler`를 피드 도메인별 독립 스케줄러로 분리하는 설계 문서.
> 단일 바이너리는 유지하되, 피드별 독립 스케줄/재시도/장애 격리를 확보한다.

---

## 1. 현재 구조 (AS-IS)

```
scraper-app (단일 프로세스)
└── ScraperScheduler (1개)
    └── run_cycle()
        ├── update_expired_events()      ← 전체 이벤트 대상
        ├── scraper.scrape_and_store()   ← event + news-JP + news-EN 혼합
        └── link_checker.check_stale_links() ← 전체 대상
```

### 문제점

| 항목 | 설명 |
|------|------|
| **스케줄 단일화** | `scrape_hour_kst: 6` 하나만 존재. 도메인별 주기 설정 불가 |
| **재시도 공유** | 글로벌 `retry_runs: VecDeque` 1개. 특정 피드 실패 → 전체 retry 정책 적용 |
| **장애 전파** | news-EN 피드 장애가 `AllFeedsFailed` 판정에 영향 |
| **순차 블로킹** | `run_cycle()` 내부가 순차 실행. 하나의 단계 지연 → 전체 사이클 블로킹 |
| **확장 불가** | 피드 종류 추가 시 (멤버 RSS, 굿즈, 콜라보 등) 전부 같은 스케줄에 묶임 |

---

## 2. 목표 구조 (TO-BE)

```
scraper-app (단일 프로세스)
├── FeedScheduler("event")     ← 독립 tokio task, 자체 cron + retry
├── FeedScheduler("news")      ← 독립 tokio task, 자체 cron + retry
└── MaintenanceScheduler       ← expired 갱신 + link_checker (별도 주기)
```

### 설계 원칙

1. **단일 바이너리 유지** — Dockerfile, 배포, 모니터링 오버헤드 없음
2. **피드별 독립 라이프사이클** — 스케줄, 재시도, 장애가 서로 격리
3. **점진적 마이그레이션** — 기존 `Scraper`, `LinkChecker`, `Repository` 변경 최소화
4. **향후 프로세스 분리 대비** — 스케줄러가 독립 단위이므로 CLI flag로 분리 가능

---

## 3. 상세 설계

### 3.1 FeedDomain 정의

```rust
// scraper-core 또는 scraper-service
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct FeedDomain {
    pub name: String,              // "event", "news"
    pub event_type: MajorEventType,
    pub feed_urls: Vec<String>,
}
```

- 기존 `ScraperConfig`의 `event_feed_url` + `news_feed_urls`를 `Vec<FeedDomain>`으로 정규화
- 향후 도메인 추가 시 config에 항목만 추가하면 스케줄러가 자동 생성

### 3.2 FeedSchedulerConfig

```rust
#[derive(Debug, Clone)]
pub struct FeedSchedulerConfig {
    pub domain: FeedDomain,
    pub cron_expr: String,         // "0 0 6 * * *" (KST)
    pub retry_delays: Vec<Duration>,
    // ScraperConfig 필드 (user_agent, max_pages 등)는 공유 또는 도메인별 override
}
```

- 기존 `SchedulerConfig.scrape_hour_kst`를 cron 표현식으로 일반화
- 피드별 retry_delays 독립 설정 가능

### 3.3 FeedScheduler

기존 `ScraperScheduler`를 피드 단위로 분리:

```rust
pub struct FeedScheduler {
    domain: FeedDomain,
    scraper: Scraper,
    repository: Repository,
    config: FeedSchedulerConfig,
    retry_runs: Mutex<VecDeque<DateTime<Utc>>>,  // 도메인별 독립
}

impl FeedScheduler {
    /// 단일 피드 도메인의 스크래핑 사이클 실행
    pub async fn run_cycle(&self) -> Result<(), ScraperError> {
        self.scraper
            .scrape_feeds(&self.repository, &self.domain.feed_urls, self.domain.event_type.clone())
            .await
            .map(|_| ())
    }

    /// 독립 스케줄 루프 (기존 ScraperScheduler::run과 동일 패턴)
    pub async fn run(&self, shutdown: CancellationToken) -> Result<(), ScraperError> {
        // ... cron 기반 next_run 계산 + retry 큐 + shutdown 핸들링
    }
}
```

### 3.4 MaintenanceScheduler

스크래핑과 무관한 유지보수 작업을 별도 스케줄러로 분리:

```rust
pub struct MaintenanceScheduler {
    repository: Repository,
    link_checker: LinkChecker,
    config: MaintenanceConfig,
}

impl MaintenanceScheduler {
    pub async fn run(&self, shutdown: CancellationToken) -> Result<(), ScraperError> {
        // expired event 갱신 + link check를 자체 주기로 실행
    }
}
```

- `update_expired_events()`: 매일 1회 (피드 스크래핑과 독립)
- `check_stale_links()`: 별도 주기 (예: 12시간마다)

### 3.5 Scraper 변경

기존 `scrape_and_store()`를 피드 단위로 호출 가능하게 리팩터링:

```rust
impl Scraper {
    /// 기존 scrape_and_store() → 특정 피드 URL 목록 + event_type만 처리
    pub async fn scrape_feeds(
        &self,
        repository: &Repository,
        feed_urls: &[String],
        event_type: MajorEventType,
    ) -> Result<usize, ScraperError> {
        // 기존 scrape_and_store 내부 로직과 동일하되,
        // feed_sources를 인자로 받음 (하드코딩된 event/news 혼합 제거)
    }
}
```

- 기존 `scrape_and_store()`는 backward compat을 위해 유지하거나, `scrape_feeds()`로 교체

### 3.6 Bootstrap 변경

```rust
// bootstrap.rs
pub async fn initialize_runtime(config: &AppConfig, shutdown: &CancellationToken) -> RuntimeInit {
    let repository = init_repository(config).await;

    // 피드별 독립 스케줄러 spawn
    let feed_handles: Vec<JoinHandle<_>> = config.feeds.iter().map(|feed_config| {
        let scheduler = FeedScheduler::new(scraper.clone(), repository.clone(), feed_config.clone());
        let token = shutdown.child_token();
        tokio::spawn(async move { scheduler.run(token).await })
    }).collect();

    // 유지보수 스케줄러 spawn
    let maintenance_handle = {
        let scheduler = MaintenanceScheduler::new(repository.clone(), link_checker, maintenance_config);
        let token = shutdown.child_token();
        tokio::spawn(async move { scheduler.run(token).await })
    };

    RuntimeInit { feed_handles, maintenance_handle, db_connected, db_monitor_handle }
}
```

### 3.7 Config 변경

```toml
# config.toml (AS-IS)
[scheduler]
scrape_hour_kst = 6

# config.toml (TO-BE)
[[feeds]]
name = "event"
type = "event"
urls = ["https://hololive.hololivepro.com/events/feed/"]
cron = "0 0 6 * * *"   # KST 06:00

[[feeds]]
name = "news"
type = "news"
urls = [
    "https://hololive.hololivepro.com/news/feed/",
    "https://hololive.hololivepro.com/en/news/feed/",
]
cron = "0 0 */6 * * *"  # KST 매 6시간

[maintenance]
expired_cron = "0 0 5 * * *"    # KST 05:00 (스크래핑 전)
link_check_cron = "0 0 */12 * * *"  # 12시간마다
```

- `[[feeds]]` 배열로 도메인 동적 추가 가능
- 기존 `[scheduler]` 단일 설정은 deprecated → 마이그레이션 헬퍼 제공

---

## 4. 변경 범위

| 레이어 | 파일 | 변경 내용 |
|--------|------|-----------|
| **core** | `model.rs` | 변경 없음 |
| **service** | `scheduler.rs` | `ScraperScheduler` → `FeedScheduler` + `MaintenanceScheduler` 분리 |
| **service** | `scraper/mod.rs` | `scrape_and_store()` → `scrape_feeds()` 파라미터화 |
| **service** | `lib.rs` | 새 모듈 export 추가 |
| **infra** | `config.rs` | `[[feeds]]` + `[maintenance]` 설정 구조 변경 |
| **app** | `bootstrap.rs` | 다중 스케줄러 spawn 로직 |
| **app** | `main.rs` | `RuntimeInit` 구조 변경에 따른 join 로직 수정 |
| **app** | `state.rs` | 헬스체크에 피드별 상태 반영 (선택) |

### 변경하지 않는 것

- `scraper-core` 모델/에러 타입
- `rss_parser.rs`, `date_extractor/`, `link_checker/` 내부 로직
- `repository.rs` DB 접근 로직
- `Dockerfile`, `docker-compose.prod.yml` (단일 바이너리 유지)

---

## 5. 마이그레이션 전략

### Phase 1: scraper 파라미터화
1. `Scraper::scrape_feeds()` 추가 (feed_urls + event_type 인자)
2. 기존 `scrape_and_store()` → `scrape_feeds()` 위임으로 변환
3. 기존 테스트 통과 확인

### Phase 2: 스케줄러 분리
1. `FeedScheduler` 구현 (기존 `ScraperScheduler` 로직 재활용)
2. `MaintenanceScheduler` 구현 (expired + link_check 분리)
3. 기존 `ScraperScheduler` deprecated (한 사이클 유지 후 제거)

### Phase 3: Config + Bootstrap 통합
1. `[[feeds]]` 설정 구조 추가, 기존 `[scheduler]` fallback 지원
2. `bootstrap.rs`에서 다중 스케줄러 spawn
3. 기존 단일 스케줄러 코드 제거

### Phase 4: 검증
1. 전체 단위 테스트 통과
2. `--run-once` 모드 동작 확인 (전체 피드 순회)
3. Docker 빌드 + 헬스체크 확인

---

## 6. 향후 확장 경로

- **피드 추가**: `config.toml`에 `[[feeds]]` 항목 추가만으로 완료
- **프로세스 분리**: CLI flag `--feed=event` 추가 → 해당 FeedScheduler만 실행
- **독립 스케일링**: 프로세스 분리 후 컨테이너 레플리카 조정
