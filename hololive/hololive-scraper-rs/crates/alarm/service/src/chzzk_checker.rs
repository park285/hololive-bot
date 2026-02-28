use std::collections::HashMap;
use std::sync::Arc;

use tracing::warn;

use chrono::{DateTime, Utc};
use futures::{StreamExt, stream};

use alarm_core::constants::NOTIFICATION_SENT_TTL;
use alarm_core::error::AlarmError;
use alarm_core::keys::chzzk_live_notified_key;
use alarm_core::model::{AlarmNotification, Stream};
use alarm_infra::chzzk::ChzzkClient;
use alarm_infra::valkey::ValkeyClient;

// ─────────────────────────────────────────────────────────────────────────────
// ChzzkChecker: Chzzk 라이브 상태 확인 + dedup 서비스
// ─────────────────────────────────────────────────────────────────────────────

/// Chzzk 라이브 방송 감지 및 알림 생성 서비스
pub struct ChzzkChecker {
    /// Chzzk API 클라이언트 (DI)
    chzzk: Arc<dyn ChzzkClient>,
    /// Valkey dedup 클라이언트 (DI)
    valkey: Arc<dyn ValkeyClient>,
}

impl ChzzkChecker {
    /// ChzzkChecker 생성
    pub fn new(chzzk: Arc<dyn ChzzkClient>, valkey: Arc<dyn ValkeyClient>) -> Self {
        Self { chzzk, valkey }
    }

    // ── 공개 API ─────────────────────────────────────────────────────────────

    /// Chzzk 채널 맵 기반 라이브 알림 생성
    /// channel_mappings: youtube_channel_id → chzzk_channel_id
    pub async fn check_chzzk_streams(
        &self,
        channel_mappings: &HashMap<String, String>,
        // 구독자 room_id 목록 조회는 호출자(스케줄러)가 담당
        subscriber_room_ids_map: &HashMap<String, Vec<String>>,
    ) -> Result<Vec<AlarmNotification>, AlarmError> {
        const CHZZK_FETCH_CONCURRENCY: usize = 16;

        if channel_mappings.is_empty() {
            return Ok(vec![]);
        }

        let mut notifications = Vec::new();
        let now = Utc::now();
        let mapping_entries: Vec<(String, String)> = channel_mappings
            .iter()
            .map(|(youtube_channel_id, chzzk_channel_id)| {
                (youtube_channel_id.clone(), chzzk_channel_id.clone())
            })
            .collect();
        let mut fetches = stream::iter(mapping_entries.into_iter().map(
            |(youtube_channel_id, chzzk_channel_id)| async move {
                let result = self.chzzk.get_live_status(&chzzk_channel_id).await;
                (youtube_channel_id, chzzk_channel_id, result)
            },
        ))
        .buffer_unordered(CHZZK_FETCH_CONCURRENCY);

        while let Some((youtube_channel_id, chzzk_channel_id, result)) = fetches.next().await {
            let stream = match result {
                Ok(s) => s,
                Err(e) => {
                    warn!(
                        chzzk_channel_id = %chzzk_channel_id,
                        error = %e,
                        "Chzzk 채널 상태 조회 실패, 건너뜀"
                    );
                    continue;
                }
            };

            let Some(stream) = stream else {
                // 오프라인 — 스킵
                continue;
            };

            // 구독자 room_id 목록 (호출자가 전달한 맵에서 조회)
            let room_ids = subscriber_room_ids_map
                .get(&youtube_channel_id)
                .map(|v| v.as_slice())
                .unwrap_or(&[]);

            if room_ids.is_empty() {
                // 구독자가 없으면 알림 생성 불필요, 단 notified 마킹은 스킵
                continue;
            }

            // 원자적 dedup claim (SET NX EX): 이미 등록된 버킷이면 스킵
            if !self
                .try_mark_chzzk_live_as_notified(&chzzk_channel_id, now)
                .await?
            {
                continue;
            }

            let mut notifs = self.build_chzzk_notifications(
                &youtube_channel_id,
                &chzzk_channel_id,
                &stream,
                room_ids,
            );
            notifications.append(&mut notifs);
        }

        Ok(notifications)
    }

    /// Chzzk 라이브 알림 발송 기록 원자적 claim (10분 버킷, SET NX EX)
    pub async fn try_mark_chzzk_live_as_notified(
        &self,
        chzzk_channel_id: &str,
        detected_at: DateTime<Utc>,
    ) -> Result<bool, AlarmError> {
        let key = chzzk_live_notified_key(chzzk_channel_id, detected_at);
        self.valkey.set_nx(&key, "1", NOTIFICATION_SENT_TTL).await
    }

    // ── 비공개 헬퍼 ──────────────────────────────────────────────────────────

    /// room_id 별 AlarmNotification 목록 생성 (minutes_until=0: 현재 라이브)
    fn build_chzzk_notifications(
        &self,
        _youtube_channel_id: &str,
        _chzzk_channel_id: &str,
        stream: &Stream,
        subscriber_room_ids: &[String],
    ) -> Vec<AlarmNotification> {
        subscriber_room_ids
            .iter()
            .map(|room_id| AlarmNotification {
                room_id: room_id.clone(),
                channel: None,
                stream: Some(stream.clone()),
                minutes_until: 0,
                users: vec![],
                schedule_change_message: String::new(),
            })
            .collect()
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use alarm_core::model::StreamStatus;
    use alarm_infra::chzzk::MockChzzkClient;
    use alarm_infra::valkey::MockValkeyClient;

    // ── 테스트 헬퍼 ──────────────────────────────────────────────────────────

    fn make_live_stream(chzzk_channel_id: &str) -> Stream {
        Stream {
            id: String::new(),
            title: "Chzzk 테스트 방송".into(),
            channel_id: String::new(),
            channel_name: "테스트 스트리머".into(),
            status: StreamStatus::Live,
            start_scheduled: None,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: Some(100),
            chzzk_channel_id: chzzk_channel_id.into(),
            chzzk_live_id: 42,
            chzzk_live_url: format!("https://chzzk.naver.com/live/{chzzk_channel_id}"),
            is_integrated: false,
            is_chzzk_only: true,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: String::new(),
            is_twitch_only: false,
        }
    }

    fn make_checker(
        chzzk: Arc<dyn alarm_infra::chzzk::ChzzkClient>,
        valkey: Arc<MockValkeyClient>,
    ) -> ChzzkChecker {
        ChzzkChecker::new(chzzk, valkey)
    }

    // ── check_chzzk_streams 테스트 ────────────────────────────────────────────

    /// 라이브 감지 + 미발송 → 알림 생성
    #[tokio::test]
    async fn check_chzzk_streams_live_not_notified_creates_notification() {
        let stream = make_live_stream("chzzk_ch1");
        let chzzk = Arc::new(MockChzzkClient::new(Some(stream)));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(chzzk, Arc::clone(&valkey));

        let mut channel_mappings = HashMap::new();
        channel_mappings.insert("UCtest".into(), "chzzk_ch1".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_a".into(), "room_b".into()]);

        let result = checker
            .check_chzzk_streams(&channel_mappings, &subscriber_map)
            .await
            .unwrap();

        // 구독자 2명 → 알림 2개
        assert_eq!(result.len(), 2);
        assert!(result.iter().all(|n| n.minutes_until == 0));
        assert!(result.iter().all(|n| n.stream.is_some()));
    }

    /// 이미 발송(10분 버킷) → 알림 생성 안 함
    #[tokio::test]
    async fn check_chzzk_streams_already_notified_skips() {
        let stream = make_live_stream("chzzk_ch2");
        let chzzk = Arc::new(MockChzzkClient::new(Some(stream)));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(chzzk, Arc::clone(&valkey));

        // 미리 dedup claim
        checker
            .try_mark_chzzk_live_as_notified("chzzk_ch2", Utc::now())
            .await
            .unwrap();

        let mut channel_mappings = HashMap::new();
        channel_mappings.insert("UCtest".into(), "chzzk_ch2".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_a".into()]);

        let result = checker
            .check_chzzk_streams(&channel_mappings, &subscriber_map)
            .await
            .unwrap();

        assert!(result.is_empty());
    }

    /// try_mark_chzzk_live_as_notified는 같은 버킷에서 원자적으로 1회만 성공
    #[tokio::test]
    async fn try_mark_chzzk_live_notified_is_atomic_per_bucket() {
        let valkey: Arc<dyn alarm_infra::valkey::ValkeyClient> = Arc::new(MockValkeyClient::new());
        let checker = ChzzkChecker::new(Arc::new(MockChzzkClient::new(None)), Arc::clone(&valkey));

        let now = Utc::now();
        let first = checker
            .try_mark_chzzk_live_as_notified("ch_abc", now)
            .await
            .unwrap();
        let second = checker
            .try_mark_chzzk_live_as_notified("ch_abc", now)
            .await
            .unwrap();
        assert!(first);
        assert!(!second);
    }

    /// check_chzzk_streams를 연속 실행하면 두 번째 호출은 원자적 dedup으로 스킵됨
    #[tokio::test]
    async fn check_chzzk_streams_second_run_skips_due_to_atomic_claim() {
        let stream = make_live_stream("chzzk_ch3");
        let chzzk = Arc::new(MockChzzkClient::new(Some(stream)));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(chzzk, Arc::clone(&valkey));

        let mut channel_mappings = HashMap::new();
        channel_mappings.insert("UCtest".into(), "chzzk_ch3".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_a".into()]);

        let first = checker
            .check_chzzk_streams(&channel_mappings, &subscriber_map)
            .await
            .unwrap();
        let second = checker
            .check_chzzk_streams(&channel_mappings, &subscriber_map)
            .await
            .unwrap();

        assert_eq!(first.len(), 1);
        assert!(second.is_empty());
    }

    /// channel_mappings가 비어있으면 빈 결과 반환
    #[tokio::test]
    async fn check_chzzk_streams_empty_mappings_returns_empty() {
        let valkey: Arc<dyn alarm_infra::valkey::ValkeyClient> = Arc::new(MockValkeyClient::new());
        let checker = ChzzkChecker::new(Arc::new(MockChzzkClient::new(None)), Arc::clone(&valkey));

        let result = checker
            .check_chzzk_streams(&HashMap::new(), &HashMap::new())
            .await
            .unwrap();

        assert!(result.is_empty());
    }
}
