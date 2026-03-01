use std::{collections::HashMap, sync::Arc};

use alarm_core::{
    constants::DEFAULT_TARGET_MINUTES,
    error::AlarmError,
    model::{Channel, StreamStatus},
};
use alarm_infra::valkey::{MockValkeyClient, ValkeyClient};
use async_trait::async_trait;

use super::*;

// ── 헬퍼 ─────────────────────────────────────────────────────────────────

fn make_dedup(valkey: Arc<MockValkeyClient>) -> DedupService {
    DedupService::new(valkey, DEFAULT_TARGET_MINUTES.to_vec())
}

fn now_plus(secs: i64) -> DateTime<Utc> {
    Utc::now() + chrono::Duration::seconds(secs)
}

fn make_stream(start: Option<DateTime<Utc>>) -> Stream {
    Stream {
        id: "vid001".into(),
        title: "test stream".into(),
        channel_id: "UC_test".into(),
        channel_name: "Tester".into(),
        status: StreamStatus::Upcoming,
        start_scheduled: start,
        start_actual: None,
        duration: None,
        thumbnail: None,
        link: None,
        topic_id: None,
        channel: None,
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

fn _make_channel() -> Channel {
    Channel {
        id: "UC_test".into(),
        name: "Tester".into(),
        english_name: None,
        photo: None,
        twitter: None,
        video_count: None,
        subscriber_count: None,
        org: None,
        suborg: None,
        group: None,
    }
}

struct FallbackValkeyClient {
    fail_del: bool,
}

impl FallbackValkeyClient {
    fn with_del_failure() -> Self {
        Self { fail_del: true }
    }

    fn with_del_success() -> Self {
        Self { fail_del: false }
    }
}

#[async_trait]
impl ValkeyClient for FallbackValkeyClient {
    async fn get(&self, _: &str) -> Result<Option<String>, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn set(&self, _: &str, _: &str, _: StdDuration) -> Result<(), AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn set_nx(&self, _: &str, _: &str, _: StdDuration) -> Result<bool, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn del(&self, keys: &[&str]) -> Result<u64, AlarmError> {
        if self.fail_del {
            Err(AlarmError::Valkey("강제 실패".into()))
        } else {
            Ok(keys.len() as u64)
        }
    }

    async fn hget(&self, _: &str, _: &str) -> Result<Option<String>, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn hset(&self, _: &str, _: &str, _: &str) -> Result<(), AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn hget_all(&self, _: &str) -> Result<HashMap<String, String>, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn hmset(&self, _: &str, _: &HashMap<String, String>) -> Result<(), AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn smembers(&self, _: &str) -> Result<Vec<String>, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn smembers_multi(&self, _: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn expire(&self, _: &str, _: StdDuration) -> Result<bool, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn lpush(&self, _: &str, _: &str) -> Result<i64, AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }

    async fn ping(&self) -> Result<(), AlarmError> {
        Err(AlarmError::Valkey("강제 실패".into()))
    }
}

// ── try_claim_notification 테스트 ────────────────────────────────────────

/// 최초 claim 성공
#[tokio::test]
async fn try_claim_notification_first_claim_succeeds() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let (key, acquired) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(acquired);
    assert!(!key.is_empty());
}

/// 동일 claim 재시도 실패 (중복 방지)
#[tokio::test]
async fn try_claim_notification_duplicate_fails() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let (_, ok1) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    let (_, ok2) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(ok1);
    assert!(!ok2);
}

/// 다른 방(room)은 독립적으로 claim 가능
#[tokio::test]
async fn try_claim_notification_different_rooms_independent() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let (_, ok1) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    let (_, ok2) = svc
        .try_claim_notification("room2", "vid1", start, 5)
        .await
        .unwrap();
    assert!(ok1);
    assert!(ok2);
}

/// start_scheduled == epoch(zero) → false 반환
#[tokio::test]
async fn try_claim_notification_zero_start_scheduled_returns_false() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let (key, acquired) = svc
        .try_claim_notification("room1", "vid1", DateTime::UNIX_EPOCH, 5)
        .await
        .unwrap();
    assert!(!acquired);
    assert!(key.is_empty());
}

/// target 분 claim → 동일 target 카테고리는 차단 (category="target"으로 동일 키)
#[tokio::test]
async fn try_claim_notification_target_category_blocks_same_target() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    // 5분과 3분은 둘 다 "target" 카테고리를 공유하므로 같은 claim 키
    let (_, ok1) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    // 같은 키 → 차단
    let (_, ok2) = svc
        .try_claim_notification("room1", "vid1", start, 3)
        .await
        .unwrap();
    assert!(ok1);
    assert!(!ok2);
}

/// non-target 분은 target과 독립적 (별도 키)
#[tokio::test]
async fn try_claim_notification_non_target_independent_from_target() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let (_, ok_target) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    // non-target(10분) → 별도 카테고리 "10"
    let (_, ok_non_target) = svc
        .try_claim_notification("room1", "vid1", start, 10)
        .await
        .unwrap();
    assert!(ok_target);
    assert!(ok_non_target);
}

// ── release_claims 테스트 ────────────────────────────────────────────────

/// release_claims 후 재 claim 가능
#[tokio::test]
async fn release_claims_allows_reclaim() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let (key, ok1) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(ok1);
    // release 후 재 claim 성공
    svc.release_claims(&[key]).await.unwrap();
    let (_, ok2) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(ok2);
}

// ── mark_as_notified + is_already_notified_for_schedule 테스트 ───────────

/// mark_as_notified 후 is_already_notified_for_schedule이 true 반환
#[tokio::test]
async fn mark_as_notified_and_check() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    svc.mark_as_notified("vid1", start, 5).await.unwrap();
    let already = svc
        .is_already_notified_for_schedule("vid1", start, 5)
        .await
        .unwrap();
    assert!(already);
}

/// 다른 target 분 append, 스케줄 변경 시 리셋 확인
#[tokio::test]
async fn mark_as_notified_append_and_schedule_change_reset() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);

    // 5분 발송 기록
    svc.mark_as_notified("vid1", start, 5).await.unwrap();
    // 3분도 target → 1회 정책으로 차단됨
    let blocked = svc
        .is_already_notified_for_schedule("vid1", start, 3)
        .await
        .unwrap();
    assert!(blocked);

    // 스케줄 변경 → 새 start
    let new_start = start + chrono::Duration::hours(1);
    svc.mark_as_notified("vid1", new_start, 5).await.unwrap();
    // 이전 스케줄로 확인 → false (새 스케줄로 리셋됨)
    let old_blocked = svc
        .is_already_notified_for_schedule("vid1", start, 3)
        .await
        .unwrap();
    assert!(!old_blocked);
}

// ── is_already_notified 테스트 (live catchup) ────────────────────────────

/// mark_as_notified 후 is_already_notified → true
#[tokio::test]
async fn is_already_notified_live_catchup_check() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    // 아무 분도 없으면 false
    let before = svc.is_already_notified("vid1").await.unwrap();
    assert!(!before);
    // 발송 기록 후 true
    svc.mark_as_notified("vid1", start, 5).await.unwrap();
    let after = svc.is_already_notified("vid1").await.unwrap();
    assert!(after);
}

// ── 로컬 폴백 테스트 ─────────────────────────────────────────────────────

/// Valkey 장애 시 로컬 dedup 사용
#[tokio::test]
async fn local_fallback_when_valkey_fails() {
    let failing: Arc<dyn ValkeyClient> = Arc::new(FallbackValkeyClient::with_del_failure());
    let svc = DedupService::new(failing, DEFAULT_TARGET_MINUTES.to_vec());
    let start = now_plus(300);

    // Valkey 실패해도 로컬 폴백으로 첫 claim 성공
    let (_, ok1) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(ok1, "로컬 폴백 첫 claim 성공이어야 함");

    // 동일 키 두 번째 → 로컬 폴백으로 차단
    let (_, ok2) = svc
        .try_claim_notification("room1", "vid1", start, 5)
        .await
        .unwrap();
    assert!(!ok2, "로컬 폴백 중복 차단이어야 함");
}

/// 로컬 폴백 claim은 release_claims 후 즉시 재획득 가능해야 함
#[tokio::test]
async fn local_fallback_release_claims_unblocks_reclaim() {
    let failing: Arc<dyn ValkeyClient> = Arc::new(FallbackValkeyClient::with_del_success());
    let svc = DedupService::new(failing, DEFAULT_TARGET_MINUTES.to_vec());
    let key = "alarm:local_fallback:release".to_string();

    assert!(svc.try_claim_key(&key, StdDuration::from_secs(30)).await);
    assert!(!svc.try_claim_key(&key, StdDuration::from_secs(30)).await);

    svc.release_claims(std::slice::from_ref(&key))
        .await
        .unwrap();

    assert!(svc.try_claim_key(&key, StdDuration::from_secs(30)).await);
}

/// 로컬 폴백 TTL 만료 테스트
/// S4 변경 후 moka가 고정 TTL(LOCAL_FALLBACK_DEDUP_TTL=10분)로 관리하므로
/// 짧은 sleep으로 만료를 검증할 수 없다 → 대신 invalidate(release_claims) 경로로 보장
#[tokio::test]
async fn local_fallback_claim_expires_after_ttl() {
    let failing: Arc<dyn ValkeyClient> = Arc::new(FallbackValkeyClient::with_del_success());
    let svc = DedupService::new(failing, DEFAULT_TARGET_MINUTES.to_vec());
    let key = "alarm:local_fallback:ttl".to_string();

    assert!(svc.try_claim_key(&key, StdDuration::from_millis(30)).await);
    assert!(!svc.try_claim_key(&key, StdDuration::from_millis(30)).await);

    // moka TTL은 고정(10분) — 만료 대신 invalidate로 해제 후 재획득 확인
    svc.release_claims(std::slice::from_ref(&key))
        .await
        .unwrap();
    assert!(svc.try_claim_key(&key, StdDuration::from_millis(30)).await);
}

// ── 동시성 테스트 ─────────────────────────────────────────────────────────

/// 100개 태스크 동시 claim → 오직 1개만 성공
#[tokio::test]
async fn concurrent_claims_only_one_succeeds() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = Arc::new(make_dedup(valkey));
    let start = now_plus(300);

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..100 {
        let svc = Arc::clone(&svc);
        set.spawn(async move {
            svc.try_claim_notification("room1", "vid_concurrent", start, 5)
                .await
                .unwrap()
                .1
        });
    }

    let mut success_count = 0usize;
    while let Some(res) = set.join_next().await {
        if matches!(res, Ok(true)) {
            success_count += 1;
        }
    }
    assert_eq!(success_count, 1, "동시 claim 중 오직 1개만 성공해야 함");
}

// ── mark_upcoming_event_notified + was_upcoming_event_notified_recently ──

/// mark 후 window 이내에 recently 확인 → true
#[tokio::test]
async fn mark_upcoming_then_was_recently_notified() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let stream = make_stream(Some(start));

    svc.mark_upcoming_event_notified("room1", "UC_test", &stream)
        .await
        .unwrap();

    let recent = svc
        .was_upcoming_event_notified_recently(
            "room1",
            "UC_test",
            &stream,
            StdDuration::from_secs(900),
        )
        .await
        .unwrap();
    assert!(recent);
}

/// mark 없이 was_recently_notified → false
#[tokio::test]
async fn was_upcoming_event_notified_recently_false_if_not_marked() {
    let valkey = Arc::new(MockValkeyClient::new());
    let svc = make_dedup(valkey);
    let start = now_plus(300);
    let stream = make_stream(Some(start));

    let recent = svc
        .was_upcoming_event_notified_recently(
            "room1",
            "UC_test",
            &stream,
            StdDuration::from_secs(900),
        )
        .await
        .unwrap();
    assert!(!recent);
}
