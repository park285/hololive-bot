use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum DeliveryOutboxKind {
    #[serde(rename = "MAJOR_EVENT_WEEKLY")]
    MajorEventWeekly,
    #[serde(rename = "MAJOR_EVENT_MONTHLY")]
    MajorEventMonthly,
    #[serde(rename = "MEMBER_NEWS_WEEKLY")]
    MemberNewsWeekly,
    #[serde(rename = "MEMBER_NEWS_MONTHLY")]
    MemberNewsMonthly,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum DeliveryOutboxStatus {
    #[serde(rename = "PENDING")]
    Pending,
    #[serde(rename = "SENT")]
    Sent,
    #[serde(rename = "FAILED")]
    Failed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotificationDeliveryOutbox {
    pub id: i64,
    pub kind: DeliveryOutboxKind,
    pub period_key: String,
    pub room_id: String,
    pub content_id: String,
    pub payload: String,
    pub status: DeliveryOutboxStatus,
    pub attempt_count: i32,
    pub next_attempt_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
    pub locked_at: Option<DateTime<Utc>>,
    pub sent_at: Option<DateTime<Utc>>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum TemplateKey {
    #[serde(rename = "OUTBOX_SHORTS")]
    OutboxShorts,
    #[serde(rename = "OUTBOX_COMMUNITY")]
    OutboxCommunity,
    #[serde(rename = "OUTBOX_VIDEO")]
    OutboxVideo,
    #[serde(rename = "OUTBOX_MILESTONE")]
    OutboxMilestone,
    #[serde(rename = "OUTBOX_VIDEO_GROUP")]
    OutboxVideoGroup,
    #[serde(rename = "OUTBOX_SHORTS_GROUP")]
    OutboxShortsGroup,
    #[serde(rename = "OUTBOX_COMMUNITY_GROUP")]
    OutboxCommunityGroup,
    #[serde(rename = "CMD_ALARM_LIST")]
    CmdAlarmList,
    #[serde(rename = "CMD_ALARM_NOTIFICATION")]
    CmdAlarmNotification,
    #[serde(rename = "CMD_ALARM_LIVE_STARTED")]
    CmdAlarmLiveStarted,
    #[serde(rename = "CMD_LIVE_STREAMS")]
    CmdLiveStreams,
    #[serde(rename = "CMD_UPCOMING_STREAMS")]
    CmdUpcomingStreams,
    #[serde(rename = "CMD_HELP")]
    CmdHelp,
    #[serde(rename = "CMD_MEMBER_DIRECTORY")]
    CmdMemberDirectory,
    #[serde(rename = "CMD_CHANNEL_SCHEDULE")]
    CmdChannelSchedule,
    #[serde(rename = "CMD_ALARM_ADDED")]
    CmdAlarmAdded,
    #[serde(rename = "CMD_ALARM_REMOVED")]
    CmdAlarmRemoved,
    #[serde(rename = "CMD_ALARM_CLEARED")]
    CmdAlarmCleared,
    #[serde(rename = "CMD_MILESTONE_ACHIEVED")]
    CmdMilestoneAchieved,
    #[serde(rename = "CMD_MILESTONE_APPROACHING")]
    CmdMilestoneApproach,
    #[serde(rename = "CMD_MAJOR_EVENT_WEEKLY_SUMMARY")]
    CmdMajorEventWeeklySummary,
    #[serde(rename = "CMD_MAJOR_EVENT_MONTHLY_SUMMARY")]
    CmdMajorEventMonthlySummary,
    #[serde(rename = "CMD_MAJOR_EVENT_SUBSCRIBED")]
    CmdMajorEventSubscribed,
    #[serde(rename = "CMD_MAJOR_EVENT_UNSUBSCRIBED")]
    CmdMajorEventUnsubscribed,
    #[serde(rename = "CMD_MAJOR_EVENT_ALREADY_SUB")]
    CmdMajorEventAlreadySub,
    #[serde(rename = "CMD_MAJOR_EVENT_NOT_SUB")]
    CmdMajorEventNotSub,
    #[serde(rename = "CMD_MAJOR_EVENT_STATUS")]
    CmdMajorEventStatus,
    #[serde(rename = "CMD_MAJOR_EVENT_USAGE")]
    CmdMajorEventUsage,
    #[serde(rename = "CMD_MEMBER_NEWS_DIGEST")]
    CmdMemberNewsDigest,
    #[serde(rename = "CMD_MEMBER_NEWS_NO_MEMBERS")]
    CmdMemberNewsNoMembers,
    #[serde(rename = "CMD_MEMBER_NEWS_SUBSCRIBED")]
    CmdMemberNewsSubscribed,
    #[serde(rename = "CMD_MEMBER_NEWS_UNSUBSCRIBED")]
    CmdMemberNewsUnsubscribed,
    #[serde(rename = "CMD_MEMBER_NEWS_ALREADY_SUB")]
    CmdMemberNewsAlreadySub,
    #[serde(rename = "CMD_MEMBER_NEWS_NOT_SUB")]
    CmdMemberNewsNotSub,
    #[serde(rename = "CMD_MEMBER_NEWS_STATUS")]
    CmdMemberNewsStatus,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotificationTemplate {
    pub id: i64,
    pub template_key: TemplateKey,
    pub channel_id: Option<String>,
    pub body: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotificationTemplateRevision {
    pub id: i64,
    pub template_id: i64,
    pub body: String,
    pub created_at: DateTime<Utc>,
}
