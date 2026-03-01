// 인프라/앱 설정 기본값 상수
// 도메인 상수는 shared_core::constants 참조

pub mod valkey_config {
    use std::time::Duration;

    pub const READY_TIMEOUT: Duration = Duration::from_secs(5);
    pub const BLOCKING_POOL_SIZE: usize = 100;
    pub const PIPELINE_MULTIPLEX: usize = 4;
}

pub mod database_config {
    use std::time::Duration;

    pub const MAX_OPEN_CONNS: usize = 25;
    pub const MAX_IDLE_CONNS: usize = 5;
    pub const CONN_MAX_LIFETIME: Duration = Duration::from_mins(5);
}

pub mod database_defaults {
    pub const HOST: &str = "postgres";
    pub const PORT: u16 = 5432;
    pub const USER: &str = "hololive_runtime";
    pub const PASSWORD: &str = "";
    pub const DATABASE: &str = "hololive";
}

pub mod server_timeout {
    use std::time::Duration;

    pub const READ_HEADER: Duration = Duration::from_secs(5);
    pub const READ: Duration = Duration::from_secs(15);
    pub const WRITE: Duration = Duration::from_mins(1);
    pub const IDLE: Duration = Duration::from_mins(1);
    pub const MAX_HEADER_BYTES: usize = 1 << 20;
}

pub mod server_config {
    pub const TRUSTED_PROXIES: &[&str] = &["127.0.0.1", "::1"];
}

pub mod cors_config {
    pub const ALLOW_METHODS: &[&str] = &["GET", "POST", "PUT", "DELETE", "OPTIONS"];
    pub const ALLOW_HEADERS: &[&str] = &[
        "Origin",
        "Content-Type",
        "Accept",
        "Authorization",
        "Sec-CH-UA",
        "Sec-CH-UA-Mobile",
        "Sec-CH-UA-Platform",
        "Sec-CH-UA-Platform-Version",
        "Sec-CH-UA-Model",
        "Sec-CH-UA-Arch",
        "Sec-CH-UA-Bitness",
        "Sec-CH-UA-Full-Version-List",
    ];
}

pub mod app_timeout {
    use std::time::Duration;

    pub const BUILD: Duration = Duration::from_secs(30);
    pub const SHUTDOWN: Duration = Duration::from_secs(10);
}

pub mod request_timeout {
    use std::time::Duration;

    pub const ADMIN_REQUEST: Duration = Duration::from_secs(10);
    pub const BOT_COMMAND: Duration = Duration::from_secs(10);
    pub const BOT_ALARM_CHECK: Duration = Duration::from_mins(2);
    pub const WEBHOOK_PROCESSING: Duration = Duration::from_secs(30);
    pub const ALARM_SERVICE: Duration = Duration::from_secs(10);
    pub const DATABASE_PING: Duration = Duration::from_secs(5);
}

pub mod holodex_transport_config {
    use std::time::Duration;

    pub const MAX_CONNS_PER_HOST: usize = 50;
    pub const MAX_IDLE_CONNS_PER_HOST: usize = 50;
    pub const IDLE_CONN_TIMEOUT: Duration = Duration::from_secs(30);
}

pub mod holodex_concurrency_config {
    use std::time::Duration;

    pub const MAX_CONCURRENT_REQUESTS: usize = 2;
    pub const REQUEST_DELAY: Duration = Duration::from_millis(500);
}

pub mod retry_config {
    use std::time::Duration;

    pub const MAX_ATTEMPTS: usize = 3;
    pub const BASE_DELAY: Duration = Duration::from_millis(500);
    pub const JITTER: Duration = Duration::from_millis(250);
}

pub mod circuit_breaker_config {
    use std::time::Duration;

    pub const FAILURE_THRESHOLD: usize = 3;
    pub const RESET_TIMEOUT: Duration = Duration::from_secs(30);
    pub const RATE_LIMIT_TIMEOUT: Duration = Duration::from_hours(1);
    pub const HEALTH_CHECK_INTERVAL: Duration = Duration::from_mins(10);
    pub const HEALTH_CHECK_TIMEOUT: Duration = Duration::from_secs(10);
}

pub mod retry_scheduler_config {
    use std::time::Duration;

    pub const DELAY: Duration = Duration::from_secs(35);
    pub const TIMEOUT: Duration = Duration::from_secs(30);
    pub const MAX_SIZE: usize = 10;
}

pub mod api_config {
    use std::time::Duration;

    pub const HOLODEX_BASE_URL: &str = "https://holodex.net/api/v2";
    pub const HOLODEX_TIMEOUT: Duration = Duration::from_secs(25);
    pub const PER_ATTEMPT_TIMEOUT: Duration = Duration::from_secs(20);
    pub const MAX_RETRY_ATTEMPTS: usize = 3;
    pub const MAX_RESPONSE_BODY_BYTES: i64 = 2 << 20;
}

pub mod iris_connection {
    use std::time::Duration;

    pub const READY_TIMEOUT: Duration = Duration::from_mins(10);
    pub const RETRY_INTERVAL: Duration = Duration::from_secs(2);
    pub const PING_TIMEOUT: Duration = Duration::from_secs(3);
}

pub use iris_webhook::IRIS_WEBHOOK_DEDUP_TTL;

mod iris_webhook {
    use std::time::Duration;

    pub const IRIS_WEBHOOK_DEDUP_TTL: Duration = Duration::from_mins(1);
}

pub mod mq_config {
    use std::time::Duration;

    pub const REPLY_STREAM_KEY: &str = "kakao:bot:reply";
    pub const REPLY_STREAM_MAX_LEN: i64 = 1000;
    pub const CONSUMER_GROUP: &str = "hololive-bot-group";
    pub const CONN_WRITE_TIMEOUT: Duration = Duration::from_secs(3);
    pub const BLOCKING_POOL_SIZE: usize = 50;
    pub const PIPELINE_MULTIPLEX: usize = 4;
    pub const DIAL_TIMEOUT: Duration = Duration::from_secs(5);
    pub const BLOCK_TIMEOUT: Duration = Duration::from_secs(5);
    pub const READ_COUNT: i64 = 50;
    pub const WORKER_COUNT: usize = 10;
    pub const IDEMPOTENCY_PROCESSING_TTL: Duration = Duration::from_mins(10);
    pub const IDEMPOTENCY_TTL: Duration = Duration::from_hours(24);
    pub const INIT_RETRY_COUNT: usize = 10;
    pub const RETRY_DELAY: Duration = Duration::from_secs(1);
}

pub mod llm_http_timeout {
    use std::time::Duration;

    pub const REQUEST: Duration = Duration::from_mins(2);
    pub const DIAL: Duration = Duration::from_secs(5);
    pub const TLS_HANDSHAKE: Duration = Duration::from_secs(5);
    pub const RESPONSE_HEADER: Duration = Duration::from_secs(15);
    pub const IDLE_CONN: Duration = Duration::from_secs(90);
}

pub mod ai_input_limits {
    pub const MAX_QUERY_LENGTH: usize = 500;
}

pub mod pagination_config {
    use std::time::Duration;

    pub const ITEMS_PER_PAGE: usize = 10;
    pub const TIMEOUT: Duration = Duration::from_mins(3);
    pub const MAX_EMBED_FIELDS: usize = 25;
}

pub mod youtube_config {
    use std::time::Duration;

    pub const DAILY_QUOTA_LIMIT: usize = 10_000;
    pub const SEARCH_QUOTA_COST: usize = 100;
    pub const CHANNELS_QUOTA_COST: usize = 1;
    pub const MAX_CHANNELS_PER_CALL: usize = 20;
    pub const MAX_CONCURRENT_REQUESTS: usize = 3;
    pub const SEARCH_MAX_RESULTS: usize = 10;
    pub const QUOTA_SAFETY_MARGIN: usize = 2_000;
    pub const CACHE_EXPIRATION: Duration = Duration::from_hours(2);
    pub const MAX_PAGE_BODY_BYTES: i64 = 8 << 20;
    pub const SCRAPER_HTTP_TIMEOUT: Duration = Duration::from_secs(15);
    pub const SCRAPER_DIAL_TIMEOUT: Duration = Duration::from_secs(5);
    pub const SCRAPER_HEADER_TIMEOUT: Duration = Duration::from_secs(12);
    pub const SCRAPER_PHASE_TIMEOUT: Duration = Duration::from_secs(45);
    pub const API_FALLBACK_TIMEOUT: Duration = Duration::from_secs(30);
    pub const CACHE_SAVE_TIMEOUT: Duration = Duration::from_secs(5);
    pub const COMMUNITY_MISSING_TTL: Duration = Duration::from_hours(24);
    pub const VIDEO_RSS_BACKOFF_TTL: Duration = Duration::from_hours(6);
}

pub mod member_cache_defaults {
    use std::time::Duration;

    pub const VALKEY_TTL: Duration = Duration::from_mins(30);
    pub const WARM_UP_CHUNK_SIZE: usize = 50;
}

pub mod websocket_config {
    use std::time::Duration;

    pub const MAX_RECONNECT_ATTEMPTS: usize = 5;
    pub const RECONNECT_DELAY: Duration = Duration::from_secs(5);
}

pub mod holodex_distributed_rate_limit_config {
    use std::time::Duration;

    pub const ENABLED: bool = true;
    pub const LIMIT: usize = 10;
    pub const WINDOW: Duration = Duration::from_secs(1);
    pub const KEY_PREFIX: &str = "ratelimit:sliding";
    pub const BUCKET_BASE: &str = "holodex:api";
}

pub mod youtube_scraper_distributed_rate_limit_config {
    use std::time::Duration;

    pub const ENABLED: bool = true;
    pub const LIMIT: usize = 1;
    pub const WINDOW: Duration = Duration::from_secs(3);
    pub const KEY_PREFIX: &str = "ratelimit:sliding";
    pub const BUCKET_BASE: &str = "youtube:scraper";
}
