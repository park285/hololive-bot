// 도메인 상수
// 인프라/앱 설정 기본값은 shared_infra::defaults 참조

use std::time::Duration;

pub const TIER1_WINDOW: Duration = Duration::from_mins(45);
pub const TIER2_WINDOW: Duration = Duration::from_hours(3);
pub const TIER3_WINDOW: Duration = Duration::from_hours(12);

pub const TIER1_INTERVAL: Duration = Duration::from_mins(1);
pub const TIER2_INTERVAL: Duration = Duration::from_mins(3);
pub const TIER3_INTERVAL: Duration = Duration::from_mins(10);
pub const TIER4_INTERVAL: Duration = Duration::from_mins(15);

pub const NO_UPCOMING_INTERVAL: Duration = Duration::from_mins(5);
pub const FULL_REFRESH_INTERVAL: Duration = Duration::from_mins(5);
pub const RECENTLY_NOTIFIED_WINDOW: Duration = Duration::from_mins(15);
pub const LIVE_CATCHUP_SUPPRESS_WINDOW: Duration = Duration::from_mins(15);

pub const NOTIFICATION_SENT_TTL: Duration = Duration::from_hours(24);
pub const NEXT_STREAM_INFO_TTL: Duration = Duration::from_hours(1);
pub const TWITCH_NOTIFICATION_TTL: Duration = Duration::from_hours(168);

pub const LOCAL_FALLBACK_DEDUP_TTL: Duration = Duration::from_mins(10);
pub const LOCAL_FALLBACK_CLEANUP_MAX_KEYS: usize = 4096;

pub const DEFAULT_TARGET_MINUTES: &[i32] = &[5, 3, 1];

pub mod cache_ttl {
    use std::time::Duration;

    pub const LIVE_STREAMS: Duration = Duration::from_mins(5);
    pub const UPCOMING_STREAMS: Duration = Duration::from_mins(5);
    pub const CHANNEL_SCHEDULE: Duration = Duration::from_mins(5);
    pub const CHANNEL_INFO: Duration = Duration::from_mins(20);
    pub const CHANNEL_SEARCH: Duration = Duration::from_mins(10);
    pub const NEXT_STREAM_INFO: Duration = Duration::from_hours(1);
    pub const NOTIFICATION_SENT: Duration = Duration::from_hours(24);
    pub const TWITCH_NOTIFICATION: Duration = Duration::from_hours(168);
}

pub mod holodex_api_params {
    pub const ORG_HOLOLIVE: &str = "Hololive";
    pub const ORG_VSPO: &str = "VSpo";
    pub const ORG_STELLIVE: &str = "Stellive";
    pub const ORG_INDIE: &str = "Indie";
    pub const ORG_ALL: &str = "all";
    pub const STATUS_LIVE: &str = "live";
    pub const STATUS_UPCOMING: &str = "upcoming";
    pub const TYPE_STREAM: &str = "stream";
    pub const TYPE_VTUBER: &str = "vtuber";
    pub const MAX_UPCOMING_HOURS: usize = 168;
    pub const DEFAULT_CHANNEL_LIMIT: usize = 50;
    pub const MAX_PAGINATION_OFFSET: usize = 500;

    pub const SYNC_TARGET_ORGS: &[&str] = &["Hololive", "VSpo", "Stellive"];
    pub const ALLOWED_FILTER_ORGS: &[&str] = &["Hololive", "VSpo", "Indie", "Stellive"];
}

pub mod string_limits {
    pub const EMBED_TITLE: usize = 256;
    pub const EMBED_DESCRIPTION: usize = 4096;
    pub const EMBED_FIELD_NAME: usize = 256;
    pub const EMBED_FIELD_VALUE: usize = 1024;
    pub const STREAM_TITLE: usize = 100;
    pub const NEXT_STREAM_TITLE: usize = 40;
}

pub mod redis_keys {
    pub const ALARM_MEMBER_NAMES: &str = "alarm:member_names";
}

pub const INDIE_CHANNEL_IDS: &[&str] = &["UCrV1Hf5r8P148idjoSfrGEQ", "UCxsZ6NCzjU_t4YSxQLBcM5A"];

pub mod official_schedule_config {
    use std::time::Duration;

    pub const BASE_URL: &str = "https://schedule.hololive.tv";
    pub const TIMEOUT: Duration = Duration::from_secs(15);
    pub const CACHE_EXPIRY: Duration = Duration::from_mins(30);
}

pub mod official_profile_config {
    use std::time::Duration;

    pub const BASE_URL: &str = "https://hololive.hololivepro.com/talents";
    pub const USER_AGENT: &str =
        "Mozilla/5.0 (compatible; HololiveKakaoBot/1.0; +https://hololive.hololivepro.com)";
    pub const ACCEPT_LANGUAGE: &str = "ja,en;q=0.8,ko;q=0.6";
    pub const REQUEST_TIMEOUT: Duration = Duration::from_secs(15);
    pub const DELAY_BETWEEN: Duration = Duration::from_millis(350);
}

pub mod major_event_config {
    use std::time::Duration;

    use chrono::Weekday;

    pub const EVENT_RSS_URL: &str = "https://hololive.hololivepro.com/events/feed/";
    pub const NEWS_RSS_URL: &str = "https://hololive.hololivepro.com/news/feed/";
    pub const NEWS_RSS_URL_EN: &str = "https://hololive.hololivepro.com/en/news/feed/";

    pub const TRUSTED_SOURCE_DOMAINS: &[&str] = &[
        "hololive.hololivepro.com",
        "hololivepro.com",
        "cover-corp.com",
        "hololive.tv",
        "schedule.hololive.tv",
        "shop.hololivepro.com",
        "hololive-official-cardgame.com",
        "aniplustv.com",
        "aniplus.co.kr",
        "animate.co.jp",
        "lawson.co.jp",
    ];

    pub const TRUSTED_SOCIAL_ACCOUNTS: &[&str] = &[
        "hololivetv",
        "hololive_en",
        "hololive_id",
        "holostarsen",
        "hololive_ocg_en",
        "aniplus_shop",
        "v_square_kr",
        "agf_korea",
    ];

    pub const SEARCH_SOURCE_SITES: &[&str] = &[
        "hololive.hololivepro.com",
        "hololivepro.com",
        "x.com",
        "twitter.com",
        "schedule.hololive.tv",
        "shop.hololivepro.com",
        "hololive-official-cardgame.com",
        "aniplustv.com",
        "aniplus.co.kr",
    ];

    pub const SEARCH_OFFICIAL_ACCOUNTS: &[&str] = &[
        "hololivetv",
        "hololive_en",
        "hololive_id",
        "HOLOSTARSen",
        "hololive_OCG_EN",
        "ANIPLUS_SHOP",
        "v_square_kr",
    ];

    pub const SEARCH_PARTNER_KEYWORDS: &[&str] =
        &["ANIPLUS", "V-SQUARE", "AGF Korea", "collaboration cafe"];

    pub const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);
    pub const SENT_KEY_TTL: Duration = Duration::from_hours(192);
    pub const SCHEDULE_HOUR_KST: u8 = 9;
    pub const SCHEDULE_WEEKDAY: Weekday = Weekday::Mon;
    pub const MAX_RETRIES: usize = 4;
    pub const RETRY_DELAY: Duration = Duration::from_secs(1);
    pub const MAX_PAGES: usize = 20;
    pub const INCREMENTAL_CURSOR_LIMIT: usize = 200;
    pub const PAGE_DELAY: Duration = Duration::from_secs(2);
    pub const USER_AGENT: &str = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36";
    pub const LINK_CHECK_BATCH_SIZE: usize = 200;
    pub const LINK_CHECK_TIMEOUT: Duration = Duration::from_secs(8);
    pub const LINK_CHECK_STALE_AFTER: Duration = Duration::from_hours(72);
    pub const SCRAPE_UPSERT_CONCURRENCY: usize = 8;
    pub const SCRAPE_HOUR_KST: u8 = 6;
    pub const SCRAPE_RETRY_DELAYS: &[Duration] = &[
        Duration::from_mins(30),
        Duration::from_hours(2),
        Duration::from_hours(6),
    ];
    pub const MONTHLY_SCHEDULE_HOUR_KST: u8 = 10;
    pub const MONTHLY_SCHEDULE_DAY: u8 = 1;
}

pub mod twitch_config {
    use std::time::Duration;

    pub const BASE_URL: &str = "https://api.twitch.tv/helix";
    pub const AUTH_URL: &str = "https://id.twitch.tv/oauth2/token";
    pub const TIMEOUT: Duration = Duration::from_secs(10);
    pub const POLL_INTERVAL: Duration = Duration::from_mins(1);
    pub const TOKEN_REFRESH_SKEW: Duration = Duration::from_mins(5);
    pub const MARKER_TTL: Duration = Duration::from_hours(168);
    pub const MAX_USERS_PER_REQUEST: usize = 100;
}
