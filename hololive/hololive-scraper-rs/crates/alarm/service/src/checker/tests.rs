use super::*;
use alarm_core::constants::DEFAULT_TARGET_MINUTES;
use alarm_core::model::{Channel, StreamStatus};
use alarm_infra::holodex::MockHolodexClient;
use alarm_infra::valkey::MockValkeyClient;
use async_trait::async_trait;
use std::{
    collections::{HashMap, HashSet},
    sync::atomic::{AtomicBool, AtomicUsize, Ordering},
    time::Duration,
};

// ── 구독 정보를 사전 주입할 수 있는 인메모리 Valkey Mock ─────────────────

/// 테스트용: smembers 결과를 사전 지정 가능한 Mock
struct PreseededValkeyClient {
    inner: MockValkeyClient,
    /// smembers 응답 사전 지정: key → members
    set_data: HashMap<String, Vec<String>>,
}

impl PreseededValkeyClient {
    fn new() -> Self {
        Self {
            inner: MockValkeyClient::new(),
            set_data: HashMap::new(),
        }
    }

    /// smembers로 반환할 데이터를 사전 등록
    fn seed_set(&mut self, key: impl Into<String>, members: Vec<String>) {
        self.set_data.insert(key.into(), members);
    }
}

struct TrackingHolodexClient {
    called: Arc<AtomicBool>,
}

impl TrackingHolodexClient {
    fn new(called: Arc<AtomicBool>) -> Self {
        Self { called }
    }
}

#[async_trait]
impl HolodexClient for TrackingHolodexClient {
    async fn get_live_streams(&self, _channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        self.called.store(true, Ordering::Relaxed);
        Ok(vec![])
    }

    async fn get_channel_streams(&self, _channel_id: &str) -> Result<Vec<Stream>, AlarmError> {
        self.called.store(true, Ordering::Relaxed);
        Ok(vec![])
    }
}

/// validate_subscribers 경로 추적/오류 주입용 Valkey mock
struct TrackingSubscriberValidationValkey {
    set_data: HashMap<String, Vec<String>>,
    fail_smembers_multi: bool,
    fail_smembers_keys: HashSet<String>,
    smembers_calls: AtomicUsize,
    smembers_multi_calls: AtomicUsize,
}

impl TrackingSubscriberValidationValkey {
    fn new(set_data: HashMap<String, Vec<String>>) -> Self {
        Self {
            set_data,
            fail_smembers_multi: false,
            fail_smembers_keys: HashSet::new(),
            smembers_calls: AtomicUsize::new(0),
            smembers_multi_calls: AtomicUsize::new(0),
        }
    }

    fn with_smembers_multi_error(mut self) -> Self {
        self.fail_smembers_multi = true;
        self
    }

    fn with_smembers_error_key(mut self, key: impl Into<String>) -> Self {
        self.fail_smembers_keys.insert(key.into());
        self
    }
}

#[async_trait]
impl ValkeyClient for TrackingSubscriberValidationValkey {
    async fn get(&self, _: &str) -> Result<Option<String>, AlarmError> {
        Ok(None)
    }

    async fn set(&self, _: &str, _: &str, _: std::time::Duration) -> Result<(), AlarmError> {
        Ok(())
    }

    async fn set_nx(&self, _: &str, _: &str, _: std::time::Duration) -> Result<bool, AlarmError> {
        Ok(true)
    }

    async fn del(&self, _: &[&str]) -> Result<u64, AlarmError> {
        Ok(0)
    }

    async fn hget(&self, _: &str, _: &str) -> Result<Option<String>, AlarmError> {
        Ok(None)
    }

    async fn hset(&self, _: &str, _: &str, _: &str) -> Result<(), AlarmError> {
        Ok(())
    }

    async fn hget_all(&self, _: &str) -> Result<HashMap<String, String>, AlarmError> {
        Ok(HashMap::new())
    }

    async fn hmset(&self, _: &str, _: &HashMap<String, String>) -> Result<(), AlarmError> {
        Ok(())
    }

    async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError> {
        self.smembers_calls.fetch_add(1, Ordering::Relaxed);
        if self.fail_smembers_keys.contains(key) {
            return Err(AlarmError::Valkey(format!(
                "forced smembers failure for {key}"
            )));
        }
        Ok(self.set_data.get(key).cloned().unwrap_or_default())
    }

    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
        self.smembers_multi_calls.fetch_add(1, Ordering::Relaxed);
        if self.fail_smembers_multi {
            return Err(AlarmError::Valkey(
                "forced smembers_multi failure".to_string(),
            ));
        }
        Ok(keys
            .iter()
            .map(|key| self.set_data.get(key).cloned().unwrap_or_default())
            .collect())
    }

    async fn expire(&self, _: &str, _: std::time::Duration) -> Result<bool, AlarmError> {
        Ok(false)
    }

    async fn lpush(&self, _: &str, _: &str) -> Result<i64, AlarmError> {
        Ok(0)
    }

    async fn ping(&self) -> Result<(), AlarmError> {
        Ok(())
    }
}

#[async_trait]
impl ValkeyClient for PreseededValkeyClient {
    async fn get(&self, key: &str) -> Result<Option<String>, AlarmError> {
        self.inner.get(key).await
    }
    async fn set(
        &self,
        key: &str,
        value: &str,
        ttl: std::time::Duration,
    ) -> Result<(), AlarmError> {
        self.inner.set(key, value, ttl).await
    }
    async fn set_nx(
        &self,
        key: &str,
        value: &str,
        ttl: std::time::Duration,
    ) -> Result<bool, AlarmError> {
        self.inner.set_nx(key, value, ttl).await
    }
    async fn del(&self, keys: &[&str]) -> Result<u64, AlarmError> {
        self.inner.del(keys).await
    }
    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, AlarmError> {
        self.inner.hget(key, field).await
    }
    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), AlarmError> {
        self.inner.hset(key, field, value).await
    }
    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, AlarmError> {
        self.inner.hget_all(key).await
    }
    async fn hmset(&self, key: &str, fields: &HashMap<String, String>) -> Result<(), AlarmError> {
        self.inner.hmset(key, fields).await
    }
    async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError> {
        // 사전 지정 데이터 우선, 없으면 inner 위임
        if let Some(members) = self.set_data.get(key) {
            return Ok(members.clone());
        }
        self.inner.smembers(key).await
    }
    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
        let mut out = Vec::with_capacity(keys.len());
        for key in keys {
            if let Some(members) = self.set_data.get(key) {
                out.push(members.clone());
            } else {
                out.push(self.inner.smembers(key).await?);
            }
        }
        Ok(out)
    }
    async fn expire(&self, key: &str, ttl: std::time::Duration) -> Result<bool, AlarmError> {
        self.inner.expire(key, ttl).await
    }
    async fn lpush(&self, key: &str, value: &str) -> Result<i64, AlarmError> {
        self.inner.lpush(key, value).await
    }
    async fn ping(&self) -> Result<(), AlarmError> {
        self.inner.ping().await
    }
}

// ── 테스트 헬퍼 ──────────────────────────────────────────────────────────

fn make_checker_with_dyn(streams: Vec<Stream>, valkey: Arc<dyn ValkeyClient>) -> YouTubeChecker {
    let holodex = Arc::new(MockHolodexClient::new(streams)) as Arc<dyn HolodexClient>;
    make_checker_with_clients(holodex, valkey)
}

fn make_checker_with_clients(
    holodex: Arc<dyn HolodexClient>,
    valkey: Arc<dyn ValkeyClient>,
) -> YouTubeChecker {
    let scheduler = Arc::new(TieredScheduler::new());
    let dedup = Arc::new(DedupService::new(
        Arc::clone(&valkey),
        DEFAULT_TARGET_MINUTES.to_vec(),
    ));
    YouTubeChecker::new(
        holodex,
        Arc::clone(&valkey),
        scheduler,
        dedup,
        DEFAULT_TARGET_MINUTES.to_vec(),
    )
}

fn make_checker(streams: Vec<Stream>, valkey: Arc<MockValkeyClient>) -> YouTubeChecker {
    make_checker_with_dyn(streams, valkey as Arc<dyn ValkeyClient>)
}

fn make_stream(
    id: &str,
    channel_id: &str,
    status: StreamStatus,
    start_scheduled: Option<DateTime<Utc>>,
    start_actual: Option<DateTime<Utc>>,
) -> Stream {
    Stream {
        id: id.into(),
        title: format!("테스트 방송 {id}"),
        channel_id: channel_id.into(),
        channel_name: "채널명".into(),
        status,
        start_scheduled,
        start_actual,
        duration: None,
        thumbnail: None,
        link: None,
        topic_id: None,
        channel: Some(Channel {
            id: channel_id.into(),
            name: "채널명".into(),
            english_name: None,
            photo: None,
            twitter: None,
            video_count: None,
            subscriber_count: None,
            org: None,
            suborg: None,
            group: None,
        }),
        viewer_count: None,
        chzzk_channel_id: String::new(),
        chzzk_live_id: 0,
        chzzk_live_url: String::new(),
        is_integrated: false,
        is_chzzk_only: false,
        twitch_user_id: String::new(),
        twitch_user_login: String::new(),
        twitch_stream_id: String::new(),
        twitch_live_url: String::new(),
        is_twitch_only: false,
    }
}

// ── filter_upcoming_streams 테스트 ───────────────────────────────────────

/// upcoming + 미래 시작 → 포함 / live, past, 과거 start → 제외
#[test]
fn filter_upcoming_streams_future_only() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], valkey);

    let now = Utc::now();
    let streams = vec![
        make_stream(
            "v1",
            "UC_A",
            StreamStatus::Upcoming,
            Some(now + chrono::Duration::minutes(10)),
            None,
        ), // 포함
        make_stream(
            "v2",
            "UC_A",
            StreamStatus::Live,
            Some(now + chrono::Duration::minutes(10)),
            None,
        ), // 제외 (live)
        make_stream(
            "v3",
            "UC_A",
            StreamStatus::Past,
            Some(now + chrono::Duration::minutes(10)),
            None,
        ), // 제외 (past)
        make_stream(
            "v4",
            "UC_A",
            StreamStatus::Upcoming,
            Some(now - chrono::Duration::minutes(5)),
            None,
        ), // 제외 (과거)
        make_stream("v5", "UC_A", StreamStatus::Upcoming, None, None), // 제외 (start 없음)
    ];

    let result = checker.filter_upcoming_streams(&streams);
    assert_eq!(result.len(), 1);
    assert_eq!(result[0].id, "v1");
}

// ── filter_live_catchup 테스트 ───────────────────────────────────────────

/// live + 6분 이내 시작 → 포함 / 10분 전 시작 → 제외
#[test]
fn filter_live_catchup_within_window() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], valkey);

    let now = Utc::now();
    let streams = vec![
        make_stream(
            "v1",
            "UC_A",
            StreamStatus::Live,
            None,
            Some(now - chrono::Duration::seconds(30)),
        ), // 포함 (30초 전)
        make_stream(
            "v2",
            "UC_A",
            StreamStatus::Live,
            None,
            Some(now - chrono::Duration::minutes(5)),
        ), // 포함 (5분 전)
        make_stream(
            "v3",
            "UC_A",
            StreamStatus::Live,
            None,
            Some(now - chrono::Duration::minutes(10)),
        ), // 제외 (10분 전)
        make_stream(
            "v4",
            "UC_A",
            StreamStatus::Upcoming,
            None,
            Some(now - chrono::Duration::seconds(30)),
        ), // 제외 (upcoming)
    ];

    let result = checker.filter_live_catchup(&streams);
    assert_eq!(result.len(), 2);
    let ids: Vec<&str> = result.iter().map(|s| s.id.as_str()).collect();
    assert!(ids.contains(&"v1"));
    assert!(ids.contains(&"v2"));
}

/// live + start_actual 없고 start_scheduled 사용
#[test]
fn filter_live_catchup_uses_start_scheduled_fallback() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], valkey);

    let now = Utc::now();
    // start_actual 없음 → start_scheduled로 판정
    let s = make_stream(
        "v1",
        "UC_A",
        StreamStatus::Live,
        Some(now - chrono::Duration::minutes(2)), // scheduled 2분 전
        None,
    );
    // E0716 회피: 임시값을 바인딩에 먼저 저장
    let binding = [s];
    let result = checker.filter_live_catchup(&binding);
    assert_eq!(result.len(), 1);
}

// ── detect_schedule_change 테스트 ────────────────────────────────────────

/// 저장된 스케줄 != 현재 스케줄 → 변경 메시지 + claim
#[tokio::test]
async fn detect_schedule_change_different_schedule_returns_message() {
    use alarm_core::model::NotifiedData;

    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], Arc::clone(&valkey));

    let old_start = Utc::now() + chrono::Duration::hours(1);
    let new_start = old_start + chrono::Duration::hours(1); // 늦춰짐

    // NotifiedData 저장 (기존 스케줄)
    let data = NotifiedData {
        start_scheduled: old_start.to_rfc3339(),
        sent_at: HashMap::new(),
    };
    let key = alarm_core::keys::notified_key("vid1");
    let json = serde_json::to_string(&data).unwrap();
    valkey
        .set(&key, &json, std::time::Duration::from_secs(3600))
        .await
        .unwrap();

    let stream = make_stream(
        "vid1",
        "UC_A",
        StreamStatus::Upcoming,
        Some(new_start),
        None,
    );

    let msg = checker.detect_schedule_change(&stream).await.unwrap();
    assert_eq!(msg, "일정이 늦춰졌습니다.");
}

/// 저장된 스케줄 == 현재 스케줄 → 빈 문자열
#[tokio::test]
async fn detect_schedule_change_same_schedule_returns_empty() {
    use alarm_core::model::NotifiedData;

    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], Arc::clone(&valkey));

    // 정확히 같은 분(초 단위까지 동일)
    let start = Utc::now() + chrono::Duration::hours(1);

    let data = NotifiedData {
        start_scheduled: start.to_rfc3339(),
        sent_at: HashMap::new(),
    };
    let key = alarm_core::keys::notified_key("vid2");
    let json = serde_json::to_string(&data).unwrap();
    valkey
        .set(&key, &json, std::time::Duration::from_secs(3600))
        .await
        .unwrap();

    let stream = make_stream("vid2", "UC_A", StreamStatus::Upcoming, Some(start), None);

    let msg = checker.detect_schedule_change(&stream).await.unwrap();
    assert!(msg.is_empty());
}

/// NotifiedData 이력 없음 → 빈 문자열 (변경 아님)
#[tokio::test]
async fn detect_schedule_change_no_history_returns_empty() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], valkey);

    let stream = make_stream(
        "vid3",
        "UC_A",
        StreamStatus::Upcoming,
        Some(Utc::now() + chrono::Duration::hours(1)),
        None,
    );

    let msg = checker.detect_schedule_change(&stream).await.unwrap();
    assert!(msg.is_empty());
}

#[tokio::test]
async fn validate_subscribers_uses_smembers_multi_batch_path() {
    let valkey = Arc::new(TrackingSubscriberValidationValkey::new(HashMap::from([
        ("alarm:room1".to_string(), vec!["UC_A".to_string()]),
        ("alarm:room2".to_string(), vec!["UC_B".to_string()]),
        (
            "alarm:room3".to_string(),
            vec!["UC_A".to_string(), "UC_Z".to_string()],
        ),
    ])));
    let checker = make_checker_with_dyn(vec![], Arc::clone(&valkey) as Arc<dyn ValkeyClient>);

    let result = checker
        .validate_subscribers(
            "UC_A",
            &[
                "room1".to_string(),
                "room2".to_string(),
                "room3".to_string(),
            ],
        )
        .await
        .unwrap();

    assert_eq!(result, vec!["room1".to_string(), "room3".to_string()]);
    assert_eq!(valkey.smembers_multi_calls.load(Ordering::Relaxed), 1);
    assert_eq!(valkey.smembers_calls.load(Ordering::Relaxed), 0);
}

#[tokio::test]
async fn validate_subscribers_falls_back_to_single_lookup_on_batch_error() {
    let valkey = Arc::new(
        TrackingSubscriberValidationValkey::new(HashMap::from([
            ("alarm:room1".to_string(), vec!["UC_A".to_string()]),
            ("alarm:room2".to_string(), vec!["UC_B".to_string()]),
            ("alarm:room3".to_string(), vec!["UC_A".to_string()]),
        ]))
        .with_smembers_multi_error()
        .with_smembers_error_key("alarm:room3"),
    );
    let checker = make_checker_with_dyn(vec![], Arc::clone(&valkey) as Arc<dyn ValkeyClient>);

    let result = checker
        .validate_subscribers(
            "UC_A",
            &[
                "room1".to_string(),
                "room2".to_string(),
                "room3".to_string(),
            ],
        )
        .await
        .unwrap();

    assert_eq!(result, vec!["room1".to_string()]);
    assert_eq!(valkey.smembers_multi_calls.load(Ordering::Relaxed), 1);
    assert_eq!(valkey.smembers_calls.load(Ordering::Relaxed), 3);
}

// ── create_notification 테스트 ───────────────────────────────────────────

/// target 분(5분 전) → 알림 생성
#[tokio::test]
async fn create_notification_target_minute_creates_notification() {
    let mut seeded = PreseededValkeyClient::new();
    // room1이 UC_A를 구독 중 (alarm:room1 set에 UC_A 포함)
    seeded.seed_set("alarm:room1", vec!["UC_A".to_string()]);
    let valkey: Arc<dyn ValkeyClient> = Arc::new(seeded);

    let checker = make_checker_with_dyn(vec![], Arc::clone(&valkey));

    // 정확히 5분 후 시작
    let start = Utc::now() + chrono::Duration::minutes(5);
    let stream = make_stream("vid1", "UC_A", StreamStatus::Upcoming, Some(start), None);

    let result = checker
        .create_notification(&stream, "UC_A", &["room1".to_string()])
        .await
        .unwrap();

    assert_eq!(result.len(), 1);
    assert_eq!(result[0].room_id, "room1");
    assert_eq!(result[0].minutes_until, 5);
}

/// non-target 분, 일정 변경 없음 → 빈 결과
#[tokio::test]
async fn create_notification_non_target_no_change_returns_empty() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], valkey);

    // 10분 후 시작 (non-target)
    let start = Utc::now() + chrono::Duration::minutes(10);
    let stream = make_stream("vid2", "UC_A", StreamStatus::Upcoming, Some(start), None);

    let result = checker
        .create_notification(&stream, "UC_A", &["room1".to_string()])
        .await
        .unwrap();

    assert!(result.is_empty());
}

#[tokio::test]
async fn check_upcoming_streams_with_zero_budget_skips_holodex_calls() {
    let mut seeded = PreseededValkeyClient::new();
    seeded.seed_set(
        alarm_core::keys::ALARM_CHANNEL_REGISTRY_KEY,
        vec!["UC_A".to_string()],
    );
    seeded.seed_set(
        format!(
            "{}{}",
            alarm_core::keys::CHANNEL_SUBSCRIBERS_KEY_PREFIX,
            "UC_A"
        ),
        vec!["room1".to_string()],
    );
    let valkey: Arc<dyn ValkeyClient> = Arc::new(seeded);

    let called = Arc::new(AtomicBool::new(false));
    let holodex =
        Arc::new(TrackingHolodexClient::new(Arc::clone(&called))) as Arc<dyn HolodexClient>;
    let checker = make_checker_with_clients(holodex, valkey);

    let result = checker
        .check_upcoming_streams_with_budget(Some(Duration::ZERO))
        .await
        .expect("budgeted check should return partial/empty result without failing");

    assert!(result.is_empty());
    assert!(
        !called.load(Ordering::Relaxed),
        "Holodex should not be called after budget is already exhausted"
    );
}

// ── create_live_catchup_notification 테스트 ──────────────────────────────

/// 발송 이력 없음 + suppress 없음 → catch-up 알림 생성
#[tokio::test]
async fn create_live_catchup_notification_not_notified_creates() {
    let mut seeded = PreseededValkeyClient::new();
    // room1이 UC_A를 구독 중
    seeded.seed_set("alarm:room1", vec!["UC_A".to_string()]);
    let valkey: Arc<dyn ValkeyClient> = Arc::new(seeded);

    let checker = make_checker_with_dyn(vec![], Arc::clone(&valkey));

    let now = Utc::now();
    let stream = make_stream(
        "vid1",
        "UC_A",
        StreamStatus::Live,
        Some(now - chrono::Duration::seconds(30)),
        Some(now - chrono::Duration::seconds(30)),
    );

    let result = checker
        .create_live_catchup_notification(&stream, "UC_A", &["room1".to_string()])
        .await
        .unwrap();

    assert_eq!(result.len(), 1);
    assert_eq!(result[0].minutes_until, 0);
}

/// 이미 알림 발송 이력 있음 → 빈 결과
#[tokio::test]
async fn create_live_catchup_notification_already_notified_returns_empty() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], Arc::clone(&valkey));

    let now = Utc::now();
    let start = now - chrono::Duration::seconds(30);
    let stream = make_stream("vid2", "UC_A", StreamStatus::Live, Some(start), Some(start));

    // 발송 이력 기록
    checker
        .dedup
        .mark_as_notified(&stream.id, start, 5)
        .await
        .unwrap();

    let result = checker
        .create_live_catchup_notification(&stream, "UC_A", &["room1".to_string()])
        .await
        .unwrap();

    assert!(result.is_empty());
}

/// HSET 형식으로 저장된 NotifiedData에서 일정 변경 감지 → 메시지 반환
/// mark_as_notified (HSET 저장) → detect_schedule_change 경로가 WRONGTYPE 없이 동작함을 검증
#[tokio::test]
async fn detect_schedule_change_with_hset_data_returns_message() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], Arc::clone(&valkey));

    let old_start = Utc::now() + chrono::Duration::hours(1);
    let new_start = old_start + chrono::Duration::hours(2); // 늦춰짐

    // HSET 형식으로 이전 스케줄 기록 (실제 Valkey에서 발생하는 WRONGTYPE 재현 경로)
    checker
        .dedup
        .mark_as_notified("hset_vid1", old_start, 5)
        .await
        .unwrap();

    // 새 스케줄로 스트림 생성
    let stream = make_stream(
        "hset_vid1",
        "UC_B",
        StreamStatus::Upcoming,
        Some(new_start),
        None,
    );

    // valkey.get() 경로라면 HASH 키에 GET 시 에러 발생하지만,
    // dedup.read_notified_data()를 경유하므로 정상 동작해야 함
    let msg = checker.detect_schedule_change(&stream).await.unwrap();
    assert_eq!(msg, "일정이 늦춰졌습니다.");
}

/// HSET 형식으로 저장된 NotifiedData에서 동일 스케줄 → 빈 문자열 반환
#[tokio::test]
async fn detect_schedule_change_with_hset_data_same_schedule_returns_empty() {
    let valkey = Arc::new(MockValkeyClient::new());
    let checker = make_checker(vec![], Arc::clone(&valkey));

    let start = Utc::now() + chrono::Duration::hours(1);

    // HSET 형식으로 동일 스케줄 기록
    checker
        .dedup
        .mark_as_notified("hset_vid2", start, 3)
        .await
        .unwrap();

    let stream = make_stream(
        "hset_vid2",
        "UC_B",
        StreamStatus::Upcoming,
        Some(start),
        None,
    );

    let msg = checker.detect_schedule_change(&stream).await.unwrap();
    assert!(msg.is_empty());
}
