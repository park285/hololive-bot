use alarm_core::model::AlarmNotification;

/// 스트림 제목 반환 (stream 없으면 빈 문자열)
pub(super) fn stream_title(n: &AlarmNotification) -> &str {
    n.stream.as_ref().map(|s| s.title.as_str()).unwrap_or("")
}

/// Unicode 안전 100자 잘라내기
pub(super) fn truncate_title(title: &str) -> String {
    title.chars().take(100).collect::<String>()
}

/// 플랫폼별 URL 라인 구성
/// - Twitch 전용: "📺 Twitch: {url}"
/// - 통합(YouTube+Chzzk): "📺 YouTube: {youtube}\n📺 치지직: {chzzk}"
/// - Chzzk 전용: "📺 치지직: {url}"
/// - 기본: "🔗 {youtube_url}"
pub(super) fn build_url_line(n: &AlarmNotification) -> String {
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
