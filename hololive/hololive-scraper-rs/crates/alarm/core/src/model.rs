use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// 방송 상태 (Holodex 기준)
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum StreamStatus {
    /// 방송 진행 중
    Live,
    /// 방송 예정
    Upcoming,
    /// 방송 종료됨
    Past,
}

impl StreamStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Live => "live",
            Self::Upcoming => "upcoming",
            Self::Past => "past",
        }
    }

    /// 유효한 상태 값인지 확인 (enum 자체로 항상 유효하나, 파싱 전 문자열 검증에 활용)
    pub fn is_valid_str(s: &str) -> bool {
        matches!(s, "live" | "upcoming" | "past")
    }
}

impl std::fmt::Display for StreamStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// 알람 종류 (방송/커뮤니티/쇼츠)
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum AlarmType {
    /// 방송 시작 알람
    #[serde(rename = "LIVE")]
    Live,
    /// 커뮤니티 포스트 알람
    #[serde(rename = "COMMUNITY")]
    Community,
    /// 쇼츠 영상 알람
    #[serde(rename = "SHORTS")]
    Shorts,
}

impl AlarmType {
    /// 유효한 알람 타입인지 확인
    pub fn is_valid(&self) -> bool {
        // enum 자체로 항상 유효 — 향후 Unknown 변형 추가를 대비한 메서드
        true
    }

    /// 문자열이 유효한 알람 타입인지 확인
    pub fn is_valid_str(s: &str) -> bool {
        matches!(s, "LIVE" | "COMMUNITY" | "SHORTS")
    }

    /// 한국어 표시 이름
    pub fn display_name(&self) -> &'static str {
        match self {
            Self::Live => "방송",
            Self::Community => "커뮤니티",
            Self::Shorts => "쇼츠",
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Live => "LIVE",
            Self::Community => "COMMUNITY",
            Self::Shorts => "SHORTS",
        }
    }

    /// 전체 알람 타입 목록
    pub fn all() -> &'static [AlarmType] {
        &[AlarmType::Live, AlarmType::Community, AlarmType::Shorts]
    }
}

impl std::fmt::Display for AlarmType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// YouTube 채널 상세 정보
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Channel {
    pub id: String,
    pub name: String,
    pub english_name: Option<String>,
    pub photo: Option<String>,
    pub twitter: Option<String>,
    pub video_count: Option<i32>,
    pub subscriber_count: Option<i32>,
    pub org: Option<String>,
    pub suborg: Option<String>,
    pub group: Option<String>,
}

impl Channel {
    /// 표시 이름 반환 (영문 이름 우선)
    pub fn display_name(&self) -> &str {
        if let Some(en) = &self.english_name
            && !en.is_empty()
        {
            return en.as_str();
        }
        &self.name
    }

    /// Hololive 소속 여부 확인
    pub fn is_hololive(&self) -> bool {
        self.org.as_deref() == Some("Hololive")
    }

    /// 프로필 사진 URL 존재 여부
    pub fn has_photo(&self) -> bool {
        self.photo
            .as_deref()
            .map(|p| !p.is_empty())
            .unwrap_or(false)
    }

    /// 프로필 사진 URL 반환 (없으면 빈 문자열)
    pub fn photo_url(&self) -> &str {
        if self.has_photo() {
            self.photo.as_deref().unwrap_or("")
        } else {
            ""
        }
    }
}

/// Holodex 등에서 수집한 방송(스트림) 상세 정보
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Stream {
    pub id: String,
    pub title: String,
    pub channel_id: String,
    pub channel_name: String,
    pub status: StreamStatus,
    pub start_scheduled: Option<DateTime<Utc>>,
    pub start_actual: Option<DateTime<Utc>>,
    pub duration: Option<i32>,
    pub thumbnail: Option<String>,
    pub link: Option<String>,
    pub topic_id: Option<String>,
    pub channel: Option<Channel>,
    pub viewer_count: Option<i32>,

    // Chzzk 관련 필드
    pub chzzk_channel_id: String,
    pub chzzk_live_id: i64,
    pub chzzk_live_url: String,
    /// 동시 송출 여부 (YouTube + Chzzk)
    pub is_integrated: bool,
    /// Chzzk 단독 방송 여부
    pub is_chzzk_only: bool,

    // Twitch 관련 필드
    pub twitch_user_id: String,
    pub twitch_user_login: String,
    pub twitch_stream_id: String,
    pub twitch_live_url: String,
    /// Twitch 단독 방송 여부
    pub is_twitch_only: bool,
}

impl Stream {
    /// 방송이 현재 진행 중('live')인지 확인
    pub fn is_live(&self) -> bool {
        self.status == StreamStatus::Live
    }

    /// 방송이 예정('upcoming') 상태인지 확인
    pub fn is_upcoming(&self) -> bool {
        self.status == StreamStatus::Upcoming
    }

    /// 방송이 종료('past')되었는지 확인
    pub fn is_past(&self) -> bool {
        self.status == StreamStatus::Past
    }

    /// YouTube 시청 URL 반환 (Link 필드가 없으면 ID로 생성)
    pub fn get_youtube_url(&self) -> String {
        if let Some(link) = &self.link
            && !link.is_empty()
        {
            return link.clone();
        }
        format!("https://youtube.com/watch?v={}", self.id)
    }

    /// Chzzk Live URL 반환 (비어있으면 빈 문자열)
    pub fn get_chzzk_live_url(&self) -> &str {
        &self.chzzk_live_url
    }

    /// Twitch Live URL 반환 (비어있으면 빈 문자열)
    pub fn get_twitch_live_url(&self) -> &str {
        &self.twitch_live_url
    }

    /// YouTube 정보(ID)가 있는지 확인
    pub fn has_youtube_info(&self) -> bool {
        !self.id.is_empty()
    }

    /// 방송 시작까지 남은 시간을 '분' 단위(올림)로 계산 — 이미 지난 경우 0 반환
    pub fn minutes_until_start(&self) -> i64 {
        let Some(scheduled) = self.start_scheduled else {
            return 0;
        };
        let now = Utc::now();
        let diff = scheduled - now;
        let secs = diff.num_seconds();
        if secs <= 0 {
            return 0;
        }
        // 올림 나눗셈: ceil(secs / 60)
        (secs + 59) / 60
    }
}

/// 알림 중복 발송 방지를 위한 이력 정보
/// SentAt: 발송된 minutesUntil을 키로 기록 (예: {5: true, 3: true})
/// 스케줄 변경 시 StartScheduled 불일치 → SentAt 맵 리셋
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotifiedData {
    pub start_scheduled: String,
    pub sent_at: HashMap<i32, bool>,
}

/// 예정 알림 발송 시각을 이벤트 단위로 기록
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpcomingEventNotifiedData {
    pub notified_at: String,
}

/// 방송 시작 임박 등의 이벤트로 발송될 알림 메시지 정보
/// 여러 사용자(users)에게 동일한 내용이 전송될 수 있다.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AlarmNotification {
    pub room_id: String,
    pub channel: Option<Channel>,
    pub stream: Option<Stream>,
    pub minutes_until: i32,
    pub users: Vec<String>,
    pub schedule_change_message: String,
}

impl AlarmNotification {
    pub fn new(
        room_id: String,
        channel: Option<Channel>,
        stream: Option<Stream>,
        minutes_until: i32,
        users: Vec<String>,
        schedule_change_message: String,
    ) -> Self {
        Self {
            room_id,
            channel,
            stream,
            minutes_until,
            users,
            schedule_change_message,
        }
    }

    /// 알림 수신 사용자 수
    pub fn user_count(&self) -> usize {
        self.users.len()
    }
}

/// Valkey 큐를 통해 Go에 전달하는 알림 발송 봉투
/// Go 측에서 BRPOP → JSON 역직렬화 후 렌더링/Iris 발송을 수행한다.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AlarmQueueEnvelope {
    pub notification: AlarmNotification,
    pub claim_keys: Vec<String>,
    pub enqueued_at: String,
    pub version: u8,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_stream(status: StreamStatus) -> Stream {
        Stream {
            id: "abc123".into(),
            title: "테스트 방송".into(),
            channel_id: "UC_test".into(),
            channel_name: "테스터".into(),
            status,
            start_scheduled: None,
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

    #[test]
    fn stream_status_live() {
        let s = make_stream(StreamStatus::Live);
        assert!(s.is_live());
        assert!(!s.is_upcoming());
        assert!(!s.is_past());
    }

    #[test]
    fn stream_status_upcoming() {
        let s = make_stream(StreamStatus::Upcoming);
        assert!(!s.is_live());
        assert!(s.is_upcoming());
        assert!(!s.is_past());
    }

    #[test]
    fn stream_status_past() {
        let s = make_stream(StreamStatus::Past);
        assert!(!s.is_live());
        assert!(!s.is_upcoming());
        assert!(s.is_past());
    }

    #[test]
    fn stream_status_as_str() {
        assert_eq!(StreamStatus::Live.as_str(), "live");
        assert_eq!(StreamStatus::Upcoming.as_str(), "upcoming");
        assert_eq!(StreamStatus::Past.as_str(), "past");
    }

    #[test]
    fn stream_status_valid_str() {
        assert!(StreamStatus::is_valid_str("live"));
        assert!(StreamStatus::is_valid_str("upcoming"));
        assert!(StreamStatus::is_valid_str("past"));
        assert!(!StreamStatus::is_valid_str("unknown"));
        assert!(!StreamStatus::is_valid_str(""));
    }

    #[test]
    fn alarm_type_is_valid() {
        assert!(AlarmType::Live.is_valid());
        assert!(AlarmType::Community.is_valid());
        assert!(AlarmType::Shorts.is_valid());
    }

    #[test]
    fn alarm_type_valid_str() {
        assert!(AlarmType::is_valid_str("LIVE"));
        assert!(AlarmType::is_valid_str("COMMUNITY"));
        assert!(AlarmType::is_valid_str("SHORTS"));
        assert!(!AlarmType::is_valid_str("live"));
        assert!(!AlarmType::is_valid_str(""));
    }

    #[test]
    fn alarm_type_display_name() {
        assert_eq!(AlarmType::Live.display_name(), "방송");
        assert_eq!(AlarmType::Community.display_name(), "커뮤니티");
        assert_eq!(AlarmType::Shorts.display_name(), "쇼츠");
    }

    #[test]
    fn stream_get_youtube_url_with_link() {
        let mut s = make_stream(StreamStatus::Live);
        s.link = Some("https://youtu.be/custom".into());
        assert_eq!(s.get_youtube_url(), "https://youtu.be/custom");
    }

    #[test]
    fn stream_get_youtube_url_without_link() {
        let s = make_stream(StreamStatus::Upcoming);
        assert_eq!(s.get_youtube_url(), "https://youtube.com/watch?v=abc123");
    }

    #[test]
    fn stream_get_youtube_url_empty_link_fallback() {
        let mut s = make_stream(StreamStatus::Live);
        s.link = Some(String::new());
        assert_eq!(s.get_youtube_url(), "https://youtube.com/watch?v=abc123");
    }

    #[test]
    fn stream_minutes_until_no_schedule() {
        let s = make_stream(StreamStatus::Upcoming);
        assert_eq!(s.minutes_until_start(), 0);
    }

    #[test]
    fn stream_minutes_until_past_time() {
        let mut s = make_stream(StreamStatus::Upcoming);
        // 이미 지난 시각
        s.start_scheduled = Some(Utc::now() - chrono::Duration::hours(1));
        assert_eq!(s.minutes_until_start(), 0);
    }

    #[test]
    fn stream_minutes_until_future_exact_minute() {
        let mut s = make_stream(StreamStatus::Upcoming);
        // 정확히 5분 후
        s.start_scheduled = Some(Utc::now() + chrono::Duration::seconds(300));
        let mins = s.minutes_until_start();
        assert!((4..=6).contains(&mins), "expected ~5, got {mins}");
    }

    #[test]
    fn channel_display_name_english_preferred() {
        let c = Channel {
            id: "UC_test".into(),
            name: "일본어이름".into(),
            english_name: Some("English Name".into()),
            photo: None,
            twitter: None,
            video_count: None,
            subscriber_count: None,
            org: None,
            suborg: None,
            group: None,
        };
        assert_eq!(c.display_name(), "English Name");
    }

    #[test]
    fn channel_display_name_fallback_to_name() {
        let c = Channel {
            id: "UC_test".into(),
            name: "일본어이름".into(),
            english_name: None,
            photo: None,
            twitter: None,
            video_count: None,
            subscriber_count: None,
            org: None,
            suborg: None,
            group: None,
        };
        assert_eq!(c.display_name(), "일본어이름");
    }

    #[test]
    fn alarm_notification_user_count() {
        let n = AlarmNotification::new(
            "room1".into(),
            None,
            None,
            5,
            vec!["user1".into(), "user2".into()],
            String::new(),
        );
        assert_eq!(n.user_count(), 2);
    }

    #[test]
    fn alarm_type_all_has_three() {
        assert_eq!(AlarmType::all().len(), 3);
    }

    #[test]
    fn alarm_queue_envelope_serde_roundtrip() {
        let envelope = AlarmQueueEnvelope {
            notification: AlarmNotification::new(
                "room1".into(),
                None,
                None,
                5,
                vec!["user1".into()],
                String::new(),
            ),
            claim_keys: vec!["notified:claim:room1:vid:123:LIVE".into()],
            enqueued_at: "2026-02-25T13:00:00Z".into(),
            version: 1,
        };
        let json = serde_json::to_string(&envelope).unwrap();
        let decoded: AlarmQueueEnvelope = serde_json::from_str(&json).unwrap();
        assert_eq!(decoded.version, 1);
        assert_eq!(decoded.claim_keys.len(), 1);
        assert_eq!(decoded.notification.room_id, "room1");
        assert_eq!(decoded.enqueued_at, "2026-02-25T13:00:00Z");
    }
}
