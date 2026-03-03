use alarm_core::model::AlarmNotification;
use chrono::{DateTime, Utc};
use chrono_tz::Asia::Seoul;

mod format;
mod resolve;

use self::{
    format::{build_url_line, stream_title, truncate_title},
    resolve::{resolve_channel_name, resolve_org_prefix},
};

// ─────────────────────────────────────────────────────────────────────────────
// AlarmTemplateRenderer: 알림 메시지 렌더링
// ─────────────────────────────────────────────────────────────────────────────

/// 알림 메시지 렌더러 — 하드코딩된 포맷 문자열 사용 (DB 템플릿 불사용)
pub struct AlarmTemplateRenderer;

/// (기존 동작과 동일) 알림 메시지 렌더링 헬퍼
pub fn render_message(notification: &AlarmNotification) -> String {
    AlarmTemplateRenderer::new().render(notification)
}

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

    #[derive(Default)]
    struct StreamOverrides<'a> {
        start_scheduled: Option<chrono::DateTime<Utc>>,
        is_twitch_only: bool,
        twitch_live_url: &'a str,
        is_integrated: bool,
        is_chzzk_only: bool,
        chzzk_live_url: &'a str,
    }

    fn make_stream_with(title: &str, options: StreamOverrides<'_>) -> Stream {
        Stream {
            id: "vid001".into(),
            title: title.into(),
            channel_id: "UC_test".into(),
            channel_name: "TestCh".into(),
            status: StreamStatus::Upcoming,
            start_scheduled: options.start_scheduled,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: None,
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: options.chzzk_live_url.into(),
            is_integrated: options.is_integrated,
            is_chzzk_only: options.is_chzzk_only,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: options.twitch_live_url.into(),
            is_twitch_only: options.is_twitch_only,
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
        let stream = make_stream_with(
            "テスト配信",
            StreamOverrides {
                start_scheduled: Some(scheduled),
                ..Default::default()
            },
        );
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
        let stream = make_stream_with("아쿠아 방송", StreamOverrides::default());
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
        let stream = make_stream_with(
            "변경된 방송",
            StreamOverrides {
                start_scheduled: Some(scheduled),
                ..Default::default()
            },
        );
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
            StreamOverrides {
                start_scheduled: Some(scheduled),
                is_twitch_only: true,
                twitch_live_url: "https://twitch.tv/kuzuha",
                ..Default::default()
            },
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
            StreamOverrides {
                start_scheduled: Some(scheduled),
                is_integrated: true,
                chzzk_live_url: "https://chzzk.naver.com/live/abc",
                ..Default::default()
            },
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
            StreamOverrides {
                start_scheduled: Some(scheduled),
                is_chzzk_only: true,
                chzzk_live_url: "https://chzzk.naver.com/live/xyz",
                ..Default::default()
            },
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
        let stream = make_stream_with(
            "VSPO 방송",
            StreamOverrides {
                start_scheduled: Some(scheduled),
                ..Default::default()
            },
        );
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
            StreamOverrides {
                start_scheduled: Some(scheduled),
                ..Default::default()
            },
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
