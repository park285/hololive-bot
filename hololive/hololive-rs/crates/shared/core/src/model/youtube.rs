use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::model::{alarm::AlarmType, notification::TemplateKey};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeChannelStatsSnapshot {
    pub channel_id: String,
    pub captured_at: DateTime<Utc>,
    pub subscriber_count: i64,
    pub view_count: i64,
    pub video_count: i64,
    pub joined_date: Option<i64>,
    pub description: Option<String>,
    pub country: Option<String>,
    pub handle: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeChannelProfile {
    pub channel_id: String,
    pub avatar: Option<ThumbnailsJSON>,
    pub banner: Option<ThumbnailsJSON>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeVideo {
    pub video_id: String,
    pub channel_id: String,
    pub title: String,
    pub thumbnail: Option<ThumbnailsJSON>,
    pub duration: Option<String>,
    pub published_text: Option<String>,
    pub is_short: bool,
    pub is_live_replay: bool,
    pub view_count: Option<i64>,
    pub first_seen_at: DateTime<Utc>,
    pub last_seen_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeCommunityPost {
    pub post_id: String,
    pub channel_id: String,
    pub author_name: Option<String>,
    pub author_photo: Option<ThumbnailsJSON>,
    pub content_text: Option<String>,
    pub published_text: Option<String>,
    pub like_count: Option<i64>,
    pub comment_count: Option<i64>,
    pub images: Option<ThumbnailsJSON>,
    pub attached_video: Option<String>,
    pub first_seen_at: DateTime<Utc>,
    pub last_seen_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum WatermarkType {
    #[serde(rename = "VIDEO")]
    Video,
    #[serde(rename = "SHORT")]
    Short,
    #[serde(rename = "COMMUNITY_POST")]
    CommunityPost,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeContentWatermark {
    pub channel_id: String,
    pub watermark_type: WatermarkType,
    pub initialized: bool,
    pub last_content_id: Option<String>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum OutboxKind {
    #[serde(rename = "NEW_VIDEO")]
    NewVideo,
    #[serde(rename = "NEW_SHORT")]
    NewShort,
    #[serde(rename = "COMMUNITY_POST")]
    CommunityPost,
    #[serde(rename = "MILESTONE")]
    Milestone,
}

impl OutboxKind {
    pub fn to_alarm_type(self) -> AlarmType {
        match self {
            Self::NewShort => AlarmType::Shorts,
            Self::CommunityPost => AlarmType::Community,
            Self::NewVideo | Self::Milestone => AlarmType::Live,
        }
    }

    pub fn to_template_key(self) -> TemplateKey {
        match self {
            Self::NewVideo => TemplateKey::OutboxVideo,
            Self::NewShort => TemplateKey::OutboxShorts,
            Self::CommunityPost => TemplateKey::OutboxCommunity,
            Self::Milestone => TemplateKey::OutboxMilestone,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum OutboxStatus {
    #[serde(rename = "PENDING")]
    Pending,
    #[serde(rename = "SENT")]
    Sent,
    #[serde(rename = "FAILED")]
    Failed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeNotificationOutbox {
    pub id: i64,
    pub kind: OutboxKind,
    pub channel_id: String,
    pub content_id: String,
    pub payload: String,
    pub status: OutboxStatus,
    pub attempt_count: i32,
    pub next_attempt_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
    pub locked_at: Option<DateTime<Utc>>,
    pub sent_at: Option<DateTime<Utc>>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum LiveStatus {
    #[serde(rename = "UPCOMING")]
    Upcoming,
    #[serde(rename = "LIVE")]
    Live,
    #[serde(rename = "ENDED")]
    Ended,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeLiveSession {
    pub video_id: String,
    pub channel_id: String,
    pub status: LiveStatus,
    pub title: Option<String>,
    pub scheduled_start_time: Option<DateTime<Utc>>,
    pub started_at: Option<DateTime<Utc>>,
    pub ended_at: Option<DateTime<Utc>>,
    pub last_seen_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeLiveViewerSample {
    pub video_id: String,
    pub captured_at: DateTime<Utc>,
    pub channel_id: String,
    pub concurrent_viewers: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct YouTubeStreamStats {
    pub video_id: String,
    pub channel_id: String,
    pub started_at: Option<DateTime<Utc>>,
    pub ended_at: Option<DateTime<Utc>>,
    pub max_concurrent_viewers: Option<i32>,
    pub avg_concurrent_viewers: Option<i32>,
    pub sample_count: i32,
    pub updated_at: DateTime<Utc>,
}

pub type ThumbnailsJSON = Vec<ThumbnailEntry>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ThumbnailEntry {
    pub url: String,
    pub width: Option<i32>,
    pub height: Option<i32>,
}
