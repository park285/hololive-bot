use std::{collections::HashMap, sync::Arc};

use alarm_core::{
    constants::TWITCH_NOTIFICATION_TTL,
    error::AlarmError,
    keys::TWITCH_LIVE_NOTIFIED_KEY_PREFIX,
    model::{AlarmNotification, Stream},
};
use alarm_infra::{twitch::TwitchClient, valkey::ValkeyClient};

// ─────────────────────────────────────────────────────────────────────────────
// TwitchChecker: Twitch 라이브 상태 확인 + dedup 서비스
// ─────────────────────────────────────────────────────────────────────────────

/// Twitch 라이브 방송 감지 및 알림 생성 서비스
pub struct TwitchChecker {
    /// Twitch API 클라이언트 (DI)
    twitch: Arc<dyn TwitchClient>,
    /// Valkey dedup 클라이언트 (DI)
    valkey: Arc<dyn ValkeyClient>,
}

impl TwitchChecker {
    /// TwitchChecker 생성
    pub fn new(twitch: Arc<dyn TwitchClient>, valkey: Arc<dyn ValkeyClient>) -> Self {
        Self { twitch, valkey }
    }

    // ── 공개 API ─────────────────────────────────────────────────────────────

    /// Twitch 로그인 맵 기반 라이브 알림 생성
    /// login_mappings: twitch_user_login → youtube_channel_id
    pub async fn check_twitch_streams(
        &self,
        login_mappings: &HashMap<String, String>,
        // 구독자 room_id 목록 조회는 호출자(스케줄러)가 담당
        subscriber_room_ids_map: &HashMap<String, Vec<String>>,
    ) -> Result<Vec<AlarmNotification>, AlarmError> {
        if login_mappings.is_empty() {
            return Ok(vec![]);
        }

        // 모든 user_login을 배치 조회
        let logins: Vec<&str> = login_mappings.keys().map(|s| s.as_str()).collect();
        let live_streams = self
            .twitch
            .get_streams(&logins)
            .await
            .map_err(|e| AlarmError::Http(format!("Twitch 스트림 배치 조회 실패: {}", e)))?;

        let mut notifications = Vec::new();

        for stream in &live_streams {
            let youtube_channel_id = match login_mappings.get(&stream.twitch_user_login) {
                Some(id) => id,
                None => continue, // 맵에 없는 스트리머 — 스킵
            };

            // 구독자 room_id 목록
            let room_ids = subscriber_room_ids_map
                .get(youtube_channel_id)
                .map(|v| v.as_slice())
                .unwrap_or(&[]);

            if room_ids.is_empty() {
                // 구독자 없으면 알림/마킹 모두 스킵
                continue;
            }

            // 원자적 dedup claim (SET NX EX): 이미 등록된 stream_id면 스킵
            if !self
                .try_mark_twitch_live_as_notified(&stream.twitch_user_id, &stream.twitch_stream_id)
                .await?
            {
                continue;
            }

            let mut notifs = self.build_twitch_notifications(youtube_channel_id, stream, room_ids);
            notifications.append(&mut notifs);
        }

        Ok(notifications)
    }

    /// Twitch 라이브 알림 발송 기록 원자적 claim (stream_id 기반, 7일 TTL, SET NX EX)
    pub async fn try_mark_twitch_live_as_notified(
        &self,
        user_id: &str,
        stream_id: &str,
    ) -> Result<bool, AlarmError> {
        let key = format!("{}{user_id}:{stream_id}", TWITCH_LIVE_NOTIFIED_KEY_PREFIX);
        self.valkey.set_nx(&key, "1", TWITCH_NOTIFICATION_TTL).await
    }

    // ── 비공개 헬퍼 ──────────────────────────────────────────────────────────

    /// room_id 별 AlarmNotification 목록 생성 (minutes_until=0: 현재 라이브)
    fn build_twitch_notifications(
        &self,
        _youtube_channel_id: &str,
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
    use alarm_core::model::StreamStatus;
    use alarm_infra::{twitch::MockTwitchClient, valkey::MockValkeyClient};

    use super::*;

    // ── 테스트 헬퍼 ──────────────────────────────────────────────────────────

    fn make_twitch_stream(user_login: &str, stream_id: &str) -> Stream {
        Stream {
            id: String::new(),
            title: "Twitch 테스트 방송".into(),
            channel_id: String::new(),
            channel_name: user_login.into(),
            status: StreamStatus::Live,
            start_scheduled: None,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: Some(500),
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: String::new(),
            is_integrated: false,
            is_chzzk_only: false,
            twitch_user_id: format!("uid_{user_login}"),
            twitch_user_login: user_login.into(),
            twitch_stream_id: stream_id.into(),
            twitch_live_url: format!("https://twitch.tv/{user_login}"),
            is_twitch_only: true,
        }
    }

    fn make_checker(
        twitch: Arc<dyn alarm_infra::twitch::TwitchClient>,
        valkey: Arc<MockValkeyClient>,
    ) -> TwitchChecker {
        TwitchChecker::new(twitch, valkey)
    }

    // ── check_twitch_streams 테스트 ───────────────────────────────────────────

    /// 라이브 스트림 감지 + 미발송 → 알림 생성
    #[tokio::test]
    async fn check_twitch_streams_live_not_notified_creates_notification() {
        let stream = make_twitch_stream("streamer_a", "sid_001");
        let twitch = Arc::new(MockTwitchClient::new(vec![stream]));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(twitch, Arc::clone(&valkey));

        let mut login_mappings = HashMap::new();
        login_mappings.insert("streamer_a".into(), "UCtest".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_x".into(), "room_y".into()]);

        let result = checker
            .check_twitch_streams(&login_mappings, &subscriber_map)
            .await
            .unwrap();

        // 구독자 2명 → 알림 2개
        assert_eq!(result.len(), 2);
        assert!(result.iter().all(|n| n.minutes_until == 0));
        assert!(result.iter().all(|n| n.stream.is_some()));
    }

    /// 이미 발송(stream_id) → 알림 생성 안 함
    #[tokio::test]
    async fn check_twitch_streams_already_notified_skips() {
        let stream = make_twitch_stream("streamer_b", "sid_002");
        let twitch = Arc::new(MockTwitchClient::new(vec![stream]));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(twitch, Arc::clone(&valkey));

        // 미리 dedup claim (user_id: uid_streamer_b, stream_id: sid_002)
        checker
            .try_mark_twitch_live_as_notified("uid_streamer_b", "sid_002")
            .await
            .unwrap();

        let mut login_mappings = HashMap::new();
        login_mappings.insert("streamer_b".into(), "UCtest".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_x".into()]);

        let result = checker
            .check_twitch_streams(&login_mappings, &subscriber_map)
            .await
            .unwrap();

        assert!(result.is_empty());
    }

    /// try_mark_twitch_live_as_notified는 같은 stream_id에서 원자적으로 1회만 성공
    #[tokio::test]
    async fn try_mark_twitch_live_notified_is_atomic_per_stream() {
        let valkey: Arc<dyn alarm_infra::valkey::ValkeyClient> = Arc::new(MockValkeyClient::new());
        let checker =
            TwitchChecker::new(Arc::new(MockTwitchClient::new(vec![])), Arc::clone(&valkey));

        let first = checker
            .try_mark_twitch_live_as_notified("uid_abc", "sid_xyz")
            .await
            .unwrap();
        let second = checker
            .try_mark_twitch_live_as_notified("uid_abc", "sid_xyz")
            .await
            .unwrap();
        assert!(first);
        assert!(!second);
    }

    /// check_twitch_streams를 연속 실행하면 두 번째 호출은 원자적 dedup으로 스킵됨
    #[tokio::test]
    async fn check_twitch_streams_second_run_skips_due_to_atomic_claim() {
        let stream = make_twitch_stream("streamer_c", "sid_003");
        let twitch = Arc::new(MockTwitchClient::new(vec![stream]));
        let valkey = Arc::new(MockValkeyClient::new());
        let checker = make_checker(twitch, Arc::clone(&valkey));

        let mut login_mappings = HashMap::new();
        login_mappings.insert("streamer_c".into(), "UCtest".into());

        let mut subscriber_map: HashMap<String, Vec<String>> = HashMap::new();
        subscriber_map.insert("UCtest".into(), vec!["room_x".into()]);

        let first = checker
            .check_twitch_streams(&login_mappings, &subscriber_map)
            .await
            .unwrap();
        let second = checker
            .check_twitch_streams(&login_mappings, &subscriber_map)
            .await
            .unwrap();

        assert_eq!(first.len(), 1);
        assert!(second.is_empty());
    }

    /// login_mappings가 비어있으면 빈 결과 반환
    #[tokio::test]
    async fn check_twitch_streams_empty_mappings_returns_empty() {
        let valkey: Arc<dyn alarm_infra::valkey::ValkeyClient> = Arc::new(MockValkeyClient::new());
        let checker =
            TwitchChecker::new(Arc::new(MockTwitchClient::new(vec![])), Arc::clone(&valkey));

        let result = checker
            .check_twitch_streams(&HashMap::new(), &HashMap::new())
            .await
            .unwrap();

        assert!(result.is_empty());
    }
}
