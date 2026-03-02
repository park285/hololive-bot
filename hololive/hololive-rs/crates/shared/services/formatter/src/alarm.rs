use std::fmt::Write as _;

use chrono::{DateTime, Utc};
use shared_core::{
    error::SharedError,
    model::{AlarmNotification, AlarmType, Member},
};

use super::ResponseFormatter;

#[derive(Debug, Clone)]
pub struct NextStreamInfo {
    pub title: String,
    pub start_scheduled: Option<DateTime<Utc>>,
    pub minutes_until: Option<i64>,
}

#[derive(Debug, Clone)]
pub struct AlarmListEntry {
    pub member_name: String,
    pub channel_id: String,
    pub alarm_types: Vec<AlarmType>,
}

pub trait AlarmFormatting: Send + Sync {
    fn format_alarm_added(
        &self,
        member_name: &str,
        added: bool,
        next_stream_info: Option<&NextStreamInfo>,
    ) -> String;
    fn format_alarm_removed(&self, member_name: &str, removed: bool) -> String;
    fn format_alarm_list(&self, alarms: &[AlarmListEntry]) -> String;
    fn format_alarm_cleared(&self, count: usize) -> String;
    fn invalid_alarm_usage(&self) -> String;
    fn alarm_notification(&self, notification: &AlarmNotification) -> String;
    fn alarm_notification_group(
        &self,
        minutes_until: i32,
        notifications: &[AlarmNotification],
    ) -> String;
    fn format_milestone_achieved(
        &self,
        member_name: &str,
        milestone: &str,
    ) -> Result<String, SharedError>;
    fn format_milestone_approaching(
        &self,
        member_name: &str,
        milestone: &str,
        remaining: &str,
    ) -> Result<String, SharedError>;
    fn format_ambiguous_members(&self, candidates: &[Member]) -> String;
}

impl AlarmFormatting for ResponseFormatter {
    fn format_alarm_added(
        &self,
        member_name: &str,
        added: bool,
        next_stream_info: Option<&NextStreamInfo>,
    ) -> String {
        if !added {
            return self.decorate(&format!("이미 알람이 등록되어 있습니다: {member_name}"));
        }

        let mut message = format!("알람이 등록되었습니다: {member_name}");
        if let Some(next_stream_info) = next_stream_info {
            let schedule = next_stream_info.start_scheduled.map_or_else(
                || "미정".to_owned(),
                |value| value.format("%m-%d %H:%M UTC").to_string(),
            );
            let _ = write!(
                message,
                "\n다음 방송: {} ({schedule})",
                next_stream_info.title
            );
            if let Some(minutes_until) = next_stream_info.minutes_until {
                let _ = write!(message, "\n남은 시간: {minutes_until}분");
            }
        }

        self.decorate(&message)
    }

    fn format_alarm_removed(&self, member_name: &str, removed: bool) -> String {
        if removed {
            self.decorate(&format!("알람이 해제되었습니다: {member_name}"))
        } else {
            self.decorate(&format!("등록된 알람이 없습니다: {member_name}"))
        }
    }

    fn format_alarm_list(&self, alarms: &[AlarmListEntry]) -> String {
        if alarms.is_empty() {
            return self.decorate("등록된 알람이 없습니다.");
        }

        let lines = alarms
            .iter()
            .map(|entry| {
                let alarm_type_names = if entry.alarm_types.is_empty() {
                    "없음".to_owned()
                } else {
                    entry
                        .alarm_types
                        .iter()
                        .map(AlarmType::display_name)
                        .collect::<Vec<_>>()
                        .join(", ")
                };
                format!("- {} ({})", entry.member_name, alarm_type_names)
            })
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!("알람 목록\n{lines}"))
    }

    fn format_alarm_cleared(&self, count: usize) -> String {
        self.decorate(&format!("알람을 초기화했습니다. 삭제 수: {count}"))
    }

    fn invalid_alarm_usage(&self) -> String {
        self.decorate("사용법: !알람 [추가|제거|목록|초기화] [멤버] [방송|커뮤니티|쇼츠]")
    }

    fn alarm_notification(&self, notification: &AlarmNotification) -> String {
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

        let timing = if notification.minutes_until <= 0 {
            "지금 시작".to_owned()
        } else {
            format!("{}분 후 시작", notification.minutes_until)
        };

        self.decorate(&format!("{member_name}: {title}\n{timing}"))
    }

    fn alarm_notification_group(
        &self,
        minutes_until: i32,
        notifications: &[AlarmNotification],
    ) -> String {
        if notifications.is_empty() {
            return self.decorate("알림 항목이 없습니다.");
        }

        let header = if minutes_until <= 0 {
            "곧 시작하는 방송".to_owned()
        } else {
            format!("{minutes_until}분 후 시작하는 방송")
        };

        let body = notifications
            .iter()
            .map(|notification| {
                notification.stream.as_ref().map_or_else(
                    || "- 방송 정보 없음".to_owned(),
                    |stream| format!("- {}: {}", stream.channel_name, stream.title),
                )
            })
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!("{header}\n{body}"))
    }

    fn format_milestone_achieved(
        &self,
        member_name: &str,
        milestone: &str,
    ) -> Result<String, SharedError> {
        Ok(self.decorate(&format!("축하합니다! {member_name} {milestone} 달성")))
    }

    fn format_milestone_approaching(
        &self,
        member_name: &str,
        milestone: &str,
        remaining: &str,
    ) -> Result<String, SharedError> {
        Ok(self.decorate(&format!(
            "{member_name} {milestone}까지 {remaining} 남았습니다"
        )))
    }

    fn format_ambiguous_members(&self, candidates: &[Member]) -> String {
        if candidates.is_empty() {
            return self.decorate("후보 멤버가 없습니다.");
        }

        let lines = candidates
            .iter()
            .map(|member| format!("- {} ({})", member.name, member.channel_id))
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!(
            "여러 후보가 있습니다. 멤버명을 구체적으로 입력해 주세요.\n{lines}"
        ))
    }
}
