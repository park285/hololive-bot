use alarm_core::model::AlarmNotification;

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

/// 채널 표시 이름 반환
/// channel.display_name() 우선, 없으면 stream.channel_name 사용
pub(super) fn resolve_channel_name(n: &AlarmNotification) -> &str {
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
pub(super) fn resolve_org_prefix(n: &AlarmNotification) -> String {
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
