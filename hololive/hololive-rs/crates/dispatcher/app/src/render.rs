use std::collections::HashMap;

use shared_core::model::AlarmNotification;
use shared_formatter::{AlarmFormatting, ResponseFormatter};
use shared_template::Renderer;
use tracing::debug;

use crate::grouping::NotificationGroup;

pub(crate) const ALARM_NOTIFICATION_TEMPLATE_KEY: &str = "CMD_ALARM_NOTIFICATION";
pub(crate) const ALARM_LIVE_STARTED_TEMPLATE_KEY: &str = "CMD_ALARM_LIVE_STARTED";

pub(crate) fn render_message(
    renderer: &Renderer,
    formatter: &ResponseFormatter,
    notification: &AlarmNotification,
) -> String {
    let template_key = if notification.minutes_until <= 0 {
        ALARM_LIVE_STARTED_TEMPLATE_KEY
    } else {
        ALARM_NOTIFICATION_TEMPLATE_KEY
    };

    let member_name = notification.channel.as_ref().map_or_else(
        || {
            notification.stream.as_ref().map_or_else(
                || "알 수 없는 멤버".to_owned(),
                |stream| stream.channel_name.clone(),
            )
        },
        |channel| channel.display_name().to_owned(),
    );

    let title = notification.stream.as_ref().map_or_else(
        || "방송 정보 없음".to_owned(),
        |stream| stream.title.clone(),
    );

    let stream_url = resolve_stream_url(notification);
    let schedule_message = notification.schedule_change_message.trim().to_owned();
    let scheduled_time_kst = resolve_scheduled_time_kst(notification);

    let context = build_go_template_context(
        &member_name,
        &title,
        &stream_url,
        notification.minutes_until,
        &schedule_message,
        &scheduled_time_kst,
    );

    match renderer.render_go(template_key, &context) {
        Ok(message) => normalize_message(&message),
        Err(error) => {
            debug!(
                error = %error,
                template_key,
                "fallback to default alarm formatter"
            );
            formatter.alarm_notification(notification)
        }
    }
}

pub(crate) fn render_group_message(
    renderer: &Renderer,
    formatter: &ResponseFormatter,
    group: &NotificationGroup,
) -> String {
    if group.notifications.len() == 1 {
        return render_message(renderer, formatter, &group.notifications[0]);
    }

    formatter.alarm_notification_group(group.minutes_until, &group.notifications)
}

fn resolve_stream_url(notification: &AlarmNotification) -> String {
    let Some(stream) = notification.stream.as_ref() else {
        return String::new();
    };

    if stream.is_twitch_only && !stream.get_twitch_live_url().is_empty() {
        return format!("📺 Twitch: {}", stream.get_twitch_live_url());
    }

    if stream.is_integrated {
        let youtube_url = stream.get_youtube_url();
        let chzzk_url = stream.get_chzzk_live_url();
        if !chzzk_url.is_empty() {
            return format!("📺 YouTube: {youtube_url}\n📺 치지직: {chzzk_url}");
        }
        return youtube_url;
    }

    if stream.is_chzzk_only {
        let chzzk_url = stream.get_chzzk_live_url();
        if !chzzk_url.is_empty() {
            return format!("📺 치지직: {chzzk_url}");
        }
    }

    stream.get_youtube_url()
}

fn resolve_scheduled_time_kst(notification: &AlarmNotification) -> String {
    let Some(scheduled_utc) = notification
        .stream
        .as_ref()
        .and_then(|stream| stream.start_scheduled)
    else {
        return String::new();
    };

    scheduled_utc
        .with_timezone(&chrono_tz::Asia::Seoul)
        .format("%H:%M")
        .to_string()
}

fn build_go_template_context(
    member_name: &str,
    title: &str,
    stream_url: &str,
    minutes_until: i32,
    schedule_message: &str,
    scheduled_time_kst: &str,
) -> HashMap<String, serde_json::Value> {
    let mut context = HashMap::new();

    context.insert(
        "ChannelName".to_owned(),
        serde_json::Value::String(member_name.to_owned()),
    );
    context.insert(
        "Title".to_owned(),
        serde_json::Value::String(title.to_owned()),
    );
    context.insert(
        "URL".to_owned(),
        serde_json::Value::String(stream_url.to_owned()),
    );
    context.insert(
        "MinutesUntil".to_owned(),
        serde_json::Value::Number(minutes_until.into()),
    );
    context.insert(
        "ScheduleMessage".to_owned(),
        serde_json::Value::String(schedule_message.to_owned()),
    );
    context.insert(
        "ScheduledTimeKST".to_owned(),
        serde_json::Value::String(scheduled_time_kst.to_owned()),
    );

    // 기본 템플릿 호환용 snake_case 필드
    context.insert(
        "channel_name".to_owned(),
        serde_json::Value::String(member_name.to_owned()),
    );
    context.insert(
        "title".to_owned(),
        serde_json::Value::String(title.to_owned()),
    );
    context.insert(
        "url".to_owned(),
        serde_json::Value::String(stream_url.to_owned()),
    );
    context.insert(
        "minutes_until".to_owned(),
        serde_json::Value::Number(minutes_until.into()),
    );
    context.insert(
        "schedule_message".to_owned(),
        serde_json::Value::String(schedule_message.to_owned()),
    );
    context.insert(
        "scheduled_time_kst".to_owned(),
        serde_json::Value::String(scheduled_time_kst.to_owned()),
    );

    context
}

fn normalize_message(message: &str) -> String {
    message
        .lines()
        .map(str::trim_end)
        .filter(|line| !line.trim().is_empty())
        .collect::<Vec<_>>()
        .join("\n")
}

#[cfg(test)]
mod tests {
    use std::{fs, path::Path};

    use chrono::{TimeZone, Utc};
    use serde::Deserialize;
    use shared_core::model::{AlarmNotification, Stream, StreamStatus};
    use shared_formatter::ResponseFormatter;

    use super::render_message;
    use crate::bootstrap::build_renderer;

    #[derive(Debug, Deserialize)]
    struct AlarmFixture {
        #[serde(rename = "AlarmNotificationUpcoming")]
        upcoming: AlarmFixtureCase,
        #[serde(rename = "AlarmNotificationLiveStarted")]
        live_started: AlarmFixtureCase,
    }

    #[derive(Debug, Deserialize)]
    struct AlarmFixtureCase {
        input: AlarmFixtureInput,
        expected_output: String,
    }

    #[derive(Debug, Deserialize)]
    struct AlarmFixtureInput {
        channel_name: String,
        minutes_until: i32,
        scheduled_time_kst: Option<String>,
        schedule_message: Option<String>,
        title: String,
        url: String,
    }

    #[tokio::test]
    async fn render_message_matches_alarm_golden_fixture() {
        let fixture = load_alarm_fixture();
        let renderer = build_renderer(None)
            .await
            .expect("build renderer with default templates");
        let formatter = ResponseFormatter::new("");

        let upcoming_notification = build_notification(&fixture.upcoming.input);
        assert_eq!(
            render_message(&renderer, &formatter, &upcoming_notification),
            fixture.upcoming.expected_output
        );

        let live_started_notification = build_notification(&fixture.live_started.input);
        assert_eq!(
            render_message(&renderer, &formatter, &live_started_notification),
            fixture.live_started.expected_output
        );
    }

    fn load_alarm_fixture() -> AlarmFixture {
        let path = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../../testdata/kakao_messages/alarm.json");
        let content = fs::read_to_string(path).expect("read alarm fixture");
        serde_json::from_str(&content).expect("parse alarm fixture")
    }

    fn build_notification(input: &AlarmFixtureInput) -> AlarmNotification {
        let start_scheduled = input
            .scheduled_time_kst
            .as_deref()
            .and_then(parse_scheduled_time_kst);

        AlarmNotification::new(
            "room-1".to_owned(),
            None,
            Some(Stream {
                id: "stream-id".to_owned(),
                title: input.title.clone(),
                channel_id: "channel-id".to_owned(),
                channel_name: input.channel_name.clone(),
                status: if input.minutes_until <= 0 {
                    StreamStatus::Live
                } else {
                    StreamStatus::Upcoming
                },
                start_scheduled,
                start_actual: None,
                duration: None,
                thumbnail: None,
                link: Some(input.url.clone()),
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
            }),
            input.minutes_until,
            Vec::new(),
            input.schedule_message.clone().unwrap_or_default(),
        )
    }

    fn parse_scheduled_time_kst(raw: &str) -> Option<chrono::DateTime<Utc>> {
        let (hour, minute) = raw.split_once(':')?;
        let hour = hour.parse::<u32>().ok()?;
        let minute = minute.parse::<u32>().ok()?;

        let kst = chrono_tz::Asia::Seoul
            .with_ymd_and_hms(2026, 3, 1, hour, minute, 0)
            .single()?;
        Some(kst.with_timezone(&Utc))
    }
}
