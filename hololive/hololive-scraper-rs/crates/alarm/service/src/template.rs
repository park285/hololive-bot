use alarm_core::model::AlarmNotification;
use chrono::{DateTime, Utc};
use chrono_tz::Asia::Seoul;

// ─────────────────────────────────────────────────────────────────────────────
// 비-Hololive 조직명 태그 매핑
// ─────────────────────────────────────────────────────────────────────────────

/// 조직명 → 표시 태그 매핑 (Hololive는 태그 없음)
fn org_tag(org: &str) -> Option<&'static str> {
    match org {
        "Nijisanji" => Some("[니지산지]"),
        "VSPO" => Some("[VSPO]"),
        "774inc" => Some("[나나시 인큐버스]"),
        "Indie" => Some("[개인세]"),
        "StellarLive" => Some("[스텔라이브]"),
        _ => Some("[기타]"),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// AlarmTemplateRenderer: 알림 메시지 렌더링
// ─────────────────────────────────────────────────────────────────────────────

/// 알림 메시지 렌더러 — 하드코딩된 포맷 문자열 사용 (DB 템플릿 불사용)
pub struct AlarmTemplateRenderer;

impl AlarmTemplateRenderer {
    pub fn new() -> Self {
        Self
    }

    /// 알림 메시지 렌더링
    /// minutes_until <= 0 이면 라이브 시작 메시지, 그 외 예정 알림 메시지
    pub fn render(&self, notification: &AlarmNotification) -> String {
        if notification.minutes_until <= 0 {
            self.render_live_started(notification)
        } else {
            self.render_upcoming(notification)
        }
    }

    /// 예정 알림 메시지 렌더링 (CMD_ALARM_NOTIFICATION)
    fn render_upcoming(&self, n: &AlarmNotification) -> String {
        let channel_name = resolve_channel_name(n);
        let org_prefix = resolve_org_prefix(n);
        let title = truncate_title(stream_title(n));
        let url_line = build_url_line(n);
        let scheduled_kst = resolve_scheduled_kst(n);

        // 일정 변경 메시지 (있으면 줄바꿈 후 추가)
        let schedule_msg = if n.schedule_change_message.is_empty() {
            String::new()
        } else {
            format!("\n{}", n.schedule_change_message)
        };

        format!(
            "🔔 방송 알림\n\n{org_prefix}{channel_name}\n⏰ {}분 후 방송 시작 ({})\n{url_line}\n📺 {title}{schedule_msg}",
            n.minutes_until, scheduled_kst,
        )
    }

    /// 라이브 시작 메시지 렌더링 (CMD_ALARM_LIVE_STARTED)
    fn render_live_started(&self, n: &AlarmNotification) -> String {
        let channel_name = resolve_channel_name(n);
        let org_prefix = resolve_org_prefix(n);
        let title = truncate_title(stream_title(n));
        let url_line = build_url_line(n);

        format!("🔴 {org_prefix}{channel_name} 방송 시작됨\n📺 {title}\n{url_line}")
    }
}

impl Default for AlarmTemplateRenderer {
    fn default() -> Self {
        Self::new()
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 헬퍼 함수
// ─────────────────────────────────────────────────────────────────────────────

/// 채널 표시 이름 반환
/// channel.display_name() 우선, 없으면 stream.channel_name 사용
fn resolve_channel_name(n: &AlarmNotification) -> &str {
    if let Some(ch) = &n.channel {
        return ch.display_name();
    }
    if let Some(stream) = &n.stream {
        return &stream.channel_name;
    }
    "알 수 없는 채널"
}

/// 비-Hololive 채널에 조직 태그 접두어 반환
/// Hololive이거나 channel 없으면 빈 문자열
fn resolve_org_prefix(n: &AlarmNotification) -> String {
    let Some(ch) = &n.channel else {
        return String::new();
    };

    // Hololive → 태그 없음
    if ch.is_hololive() {
        return String::new();
    }

    // org 없으면 태그 없음
    let Some(org) = &ch.org else {
        return String::new();
    };

    if let Some(tag) = org_tag(org) {
        format!("{tag} ")
    } else {
        String::new()
    }
}

/// 스트림 제목 반환 (stream 없으면 빈 문자열)
fn stream_title(n: &AlarmNotification) -> &str {
    n.stream.as_ref().map(|s| s.title.as_str()).unwrap_or("")
}

/// Unicode 안전 100자 잘라내기
fn truncate_title(title: &str) -> String {
    title.chars().take(100).collect::<String>()
}

/// 플랫폼별 URL 라인 구성
/// - Twitch 전용: "📺 Twitch: {url}"
/// - 통합(YouTube+Chzzk): "📺 YouTube: {youtube}\n📺 치지직: {chzzk}"
/// - Chzzk 전용: "📺 치지직: {url}"
/// - 기본: "🔗 {youtube_url}"
fn build_url_line(n: &AlarmNotification) -> String {
    let Some(stream) = &n.stream else {
        return String::new();
    };

    if stream.is_twitch_only && !stream.twitch_live_url.is_empty() {
        return format!("📺 Twitch: {}", stream.twitch_live_url);
    }

    if stream.is_integrated {
        let youtube_url = stream.get_youtube_url();
        let chzzk_url = stream.get_chzzk_live_url();
        if !chzzk_url.is_empty() {
            return format!("📺 YouTube: {youtube_url}\n📺 치지직: {chzzk_url}");
        }
        return format!("🔗 {youtube_url}");
    }

    if stream.is_chzzk_only {
        let chzzk_url = stream.get_chzzk_live_url();
        if !chzzk_url.is_empty() {
            return format!("📺 치지직: {chzzk_url}");
        }
    }

    // 기본: YouTube URL
    format!("🔗 {}", stream.get_youtube_url())
}

/// 예정 시각을 KST "HH:MM" 형식으로 변환
/// stream.start_scheduled 없으면 빈 문자열
fn resolve_scheduled_kst(n: &AlarmNotification) -> String {
    let start_utc: Option<DateTime<Utc>> = n.stream.as_ref().and_then(|s| s.start_scheduled);
    let Some(utc) = start_utc else {
        return String::new();
    };
    let kst = utc.with_timezone(&Seoul);
    format!("{}", kst.format("%H:%M"))
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use alarm_core::model::{AlarmNotification, Channel, Stream, StreamStatus};
    use chrono::{TimeZone, Utc};

    use super::*;

    // ── 테스트 헬퍼 ──────────────────────────────────────────────────────────

    fn make_channel(name: &str, english_name: Option<&str>, org: Option<&str>) -> Channel {
        Channel {
            id: "UC_test".into(),
            name: name.into(),
            english_name: english_name.map(|s| s.into()),
            photo: None,
            twitter: None,
            video_count: None,
            subscriber_count: None,
            org: org.map(|s| s.into()),
            suborg: None,
            group: None,
        }
    }

    fn make_stream_with(
        title: &str,
        start_scheduled: Option<chrono::DateTime<Utc>>,
        is_twitch_only: bool,
        twitch_live_url: &str,
        is_integrated: bool,
        is_chzzk_only: bool,
        chzzk_live_url: &str,
    ) -> Stream {
        Stream {
            id: "vid001".into(),
            title: title.into(),
            channel_id: "UC_test".into(),
            channel_name: "TestCh".into(),
            status: StreamStatus::Upcoming,
            start_scheduled,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: None,
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: chzzk_live_url.into(),
            is_integrated,
            is_chzzk_only,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: twitch_live_url.into(),
            is_twitch_only,
        }
    }

    fn make_notification(
        channel: Option<Channel>,
        stream: Option<Stream>,
        minutes_until: i32,
        schedule_change_message: &str,
    ) -> AlarmNotification {
        AlarmNotification::new(
            "room1".into(),
            channel,
            stream,
            minutes_until,
            vec![],
            schedule_change_message.into(),
        )
    }

    // ── 테스트 케이스 ─────────────────────────────────────────────────────────

    /// 1. 예정 알림: 채널 이름 + 예정 시각 포함 확인
    #[test]
    fn render_upcoming_contains_channel_name_and_scheduled_time() {
        let renderer = AlarmTemplateRenderer::new();

        // 2025-01-01 15:00 UTC → KST 00:00
        let scheduled = Utc.with_ymd_and_hms(2025, 1, 1, 15, 0, 0).unwrap();
        let ch = make_channel("Suisei Ch.", Some("Hoshimachi Suisei"), Some("Hololive"));
        let stream = make_stream_with("テスト配信", Some(scheduled), false, "", false, false, "");
        let n = make_notification(Some(ch), Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(msg.contains("Hoshimachi Suisei"), "채널 이름 포함되어야 함");
        assert!(
            msg.contains("5분 후 방송 시작"),
            "minutes_until 포함되어야 함"
        );
        // KST 00:00
        assert!(msg.contains("00:00"), "KST 시각 포함되어야 함");
        assert!(msg.contains("テスト配信"), "제목 포함되어야 함");
        assert!(msg.contains("🔔 방송 알림"), "헤더 포함되어야 함");
    }

    /// 2. 라이브 시작 알림 렌더링
    #[test]
    fn render_live_started_contains_live_header() {
        let renderer = AlarmTemplateRenderer::new();

        let ch = make_channel("Aqua Ch.", Some("Minato Aqua"), Some("Hololive"));
        let stream = make_stream_with("아쿠아 방송", None, false, "", false, false, "");
        let n = make_notification(Some(ch), Some(stream), 0, "");

        let msg = renderer.render(&n);

        assert!(msg.contains("🔴"), "라이브 헤더 이모지 포함");
        assert!(msg.contains("방송 시작됨"), "방송 시작됨 포함");
        assert!(msg.contains("Minato Aqua"), "채널 이름 포함");
    }

    /// 3. 일정 변경 메시지가 있으면 예정 알림에 추가됨
    #[test]
    fn render_upcoming_appends_schedule_change_message() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let ch = make_channel("Test Ch.", None, Some("Hololive"));
        let stream = make_stream_with("변경된 방송", Some(scheduled), false, "", false, false, "");
        let n = make_notification(Some(ch), Some(stream), 5, "일정이 늦춰졌습니다.");

        let msg = renderer.render(&n);

        assert!(
            msg.contains("일정이 늦춰졌습니다."),
            "일정 변경 메시지 포함"
        );
    }

    /// 4. Twitch 전용 스트림 → Twitch URL 포함
    #[test]
    fn render_twitch_only_uses_twitch_url() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let ch = make_channel("Kuzuha Ch.", None, Some("Nijisanji"));
        let stream = make_stream_with(
            "Twitch 방송",
            Some(scheduled),
            true,
            "https://twitch.tv/kuzuha",
            false,
            false,
            "",
        );
        let n = make_notification(Some(ch), Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(
            msg.contains("📺 Twitch: https://twitch.tv/kuzuha"),
            "Twitch URL 포함"
        );
    }

    /// 5. 통합 스트림(YouTube+Chzzk) → 두 URL 모두 포함
    #[test]
    fn render_integrated_stream_contains_both_youtube_and_chzzk_urls() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let ch = make_channel("Marine Ch.", None, Some("Hololive"));
        let stream = make_stream_with(
            "통합 방송",
            Some(scheduled),
            false,
            "",
            true,
            false,
            "https://chzzk.naver.com/live/abc",
        );
        let n = make_notification(Some(ch), Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(msg.contains("📺 YouTube:"), "YouTube URL 라인 포함");
        assert!(
            msg.contains("📺 치지직: https://chzzk.naver.com/live/abc"),
            "Chzzk URL 포함"
        );
    }

    /// 6. Chzzk 전용 스트림 → Chzzk URL만 포함
    #[test]
    fn render_chzzk_only_uses_chzzk_url() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let ch = make_channel("이세계아이돌", None, Some("Indie"));
        let stream = make_stream_with(
            "치지직 방송",
            Some(scheduled),
            false,
            "",
            false,
            true,
            "https://chzzk.naver.com/live/xyz",
        );
        let n = make_notification(Some(ch), Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(
            msg.contains("📺 치지직: https://chzzk.naver.com/live/xyz"),
            "Chzzk URL 포함"
        );
        assert!(!msg.contains("YouTube:"), "YouTube URL 없어야 함");
    }

    /// 7. 비-Hololive(VSPO) 채널 → 조직 태그 접두어 포함
    #[test]
    fn render_non_hololive_org_has_tag_prefix() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let ch = make_channel("Sena Ch.", None, Some("VSPO"));
        let stream = make_stream_with("VSPO 방송", Some(scheduled), false, "", false, false, "");
        let n = make_notification(Some(ch), Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(msg.contains("[VSPO]"), "VSPO 조직 태그 포함");
    }

    /// 8. channel 없으면 stream.channel_name 사용
    #[test]
    fn render_without_channel_uses_stream_channel_name() {
        let renderer = AlarmTemplateRenderer::new();

        let scheduled = Utc::now() + chrono::Duration::minutes(5);
        let mut stream = make_stream_with(
            "이름 없는 방송",
            Some(scheduled),
            false,
            "",
            false,
            false,
            "",
        );
        stream.channel_name = "FallbackChannel".into();
        let n = make_notification(None, Some(stream), 5, "");

        let msg = renderer.render(&n);

        assert!(
            msg.contains("FallbackChannel"),
            "stream.channel_name 사용되어야 함"
        );
    }
}
