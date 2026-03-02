use std::collections::HashMap;

use shared_core::model::{AlarmNotification, AlarmQueueEnvelope};

pub(crate) struct NotificationGroup {
    pub room_id: String,
    pub minutes_until: i32,
    pub notifications: Vec<AlarmNotification>,
    pub claim_keys: Vec<String>,
}

pub(crate) fn group_queue_envelopes(envelopes: Vec<AlarmQueueEnvelope>) -> Vec<NotificationGroup> {
    let mut groups: Vec<NotificationGroup> = Vec::new();
    let mut index = HashMap::<String, usize>::new();

    for envelope in envelopes {
        let key = build_group_key(&envelope.notification);
        if let Some(&existing_index) = index.get(&key) {
            let group = &mut groups[existing_index];
            group.minutes_until =
                merge_minutes_until(group.minutes_until, envelope.notification.minutes_until);
            group.notifications.push(envelope.notification);
            group.claim_keys.extend(envelope.claim_keys);
            continue;
        }

        let group = NotificationGroup {
            room_id: envelope.notification.room_id.clone(),
            minutes_until: envelope.notification.minutes_until,
            notifications: vec![envelope.notification],
            claim_keys: envelope.claim_keys,
        };
        groups.push(group);
        index.insert(key, groups.len() - 1);
    }

    groups
}

fn build_group_key(notification: &AlarmNotification) -> String {
    if let Some(stream) = &notification.stream
        && let Some(start_scheduled) = stream.start_scheduled
    {
        let minute_bucket = start_scheduled.timestamp() / 60;
        return format!("{}|scheduled|{minute_bucket}", notification.room_id);
    }

    format!(
        "{}|minutes|{}",
        notification.room_id, notification.minutes_until
    )
}

fn merge_minutes_until(current: i32, next: i32) -> i32 {
    if next < 0 {
        return current;
    }

    if current < 0 {
        return next;
    }

    current.min(next)
}

#[cfg(test)]
mod tests {
    use chrono::{TimeZone, Utc};
    use shared_core::model::{AlarmQueueEnvelope, Stream, StreamStatus};

    use super::group_queue_envelopes;

    #[test]
    fn groups_by_room_and_scheduled_minute() {
        let scheduled_a = Utc
            .with_ymd_and_hms(2026, 3, 1, 12, 0, 10)
            .single()
            .expect("valid timestamp");
        let scheduled_b = Utc
            .with_ymd_and_hms(2026, 3, 1, 12, 0, 59)
            .single()
            .expect("valid timestamp");

        let envelopes = vec![
            envelope("room-a", "stream-1", 5, Some(scheduled_a), "claim-1"),
            envelope("room-a", "stream-2", 3, Some(scheduled_b), "claim-2"),
            envelope("room-b", "stream-3", 7, Some(scheduled_a), "claim-3"),
        ];

        let groups = group_queue_envelopes(envelopes);

        assert_eq!(groups.len(), 2);
        assert_eq!(groups[0].room_id, "room-a");
        assert_eq!(groups[0].notifications.len(), 2);
        assert_eq!(groups[0].minutes_until, 3);
        assert_eq!(groups[0].claim_keys.len(), 2);
    }

    #[test]
    fn groups_by_minutes_when_schedule_is_missing() {
        let envelopes = vec![
            envelope("room-a", "stream-1", 5, None, "claim-1"),
            envelope("room-a", "stream-2", 5, None, "claim-2"),
            envelope("room-a", "stream-3", 3, None, "claim-3"),
        ];

        let groups = group_queue_envelopes(envelopes);
        assert_eq!(groups.len(), 2);
        assert_eq!(groups[0].notifications.len(), 2);
        assert_eq!(groups[1].notifications.len(), 1);
    }

    fn envelope(
        room_id: &str,
        stream_id: &str,
        minutes_until: i32,
        start_scheduled: Option<chrono::DateTime<Utc>>,
        claim_key: &str,
    ) -> AlarmQueueEnvelope {
        AlarmQueueEnvelope {
            notification: shared_core::model::AlarmNotification {
                room_id: room_id.to_owned(),
                channel: None,
                stream: Some(Stream {
                    id: stream_id.to_owned(),
                    title: format!("title-{stream_id}"),
                    channel_id: "channel-id".to_owned(),
                    channel_name: "channel-name".to_owned(),
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
                    chzzk_live_url: String::new(),
                    is_integrated: false,
                    is_chzzk_only: false,
                    twitch_user_id: String::new(),
                    twitch_user_login: String::new(),
                    twitch_stream_id: String::new(),
                    twitch_live_url: String::new(),
                    is_twitch_only: false,
                }),
                minutes_until,
                users: Vec::new(),
                schedule_change_message: String::new(),
            },
            claim_keys: vec![format!("notified:claim:{claim_key}")],
            enqueued_at: Utc::now().to_rfc3339(),
            version: 1,
        }
    }
}
