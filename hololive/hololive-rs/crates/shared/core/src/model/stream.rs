use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::model::channel::Channel;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum StreamStatus {
    Live,
    Upcoming,
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

    pub fn is_valid_str(s: &str) -> bool {
        matches!(s, "live" | "upcoming" | "past")
    }
}

impl std::fmt::Display for StreamStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

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

    pub chzzk_channel_id: String,
    pub chzzk_live_id: i64,
    pub chzzk_live_url: String,
    pub is_integrated: bool,
    pub is_chzzk_only: bool,

    pub twitch_user_id: String,
    pub twitch_user_login: String,
    pub twitch_stream_id: String,
    pub twitch_live_url: String,
    pub is_twitch_only: bool,
}

impl Stream {
    pub fn is_live(&self) -> bool {
        self.status == StreamStatus::Live
    }

    pub fn is_upcoming(&self) -> bool {
        self.status == StreamStatus::Upcoming
    }

    pub fn is_past(&self) -> bool {
        self.status == StreamStatus::Past
    }

    pub fn minutes_until_start(&self) -> i64 {
        let Some(start_scheduled) = self.start_scheduled else {
            return 0;
        };

        let now = Utc::now();
        let diff = start_scheduled - now;
        let secs = diff.num_seconds();
        if secs <= 0 {
            return 0;
        }

        (secs + 59) / 60
    }

    pub fn get_youtube_url(&self) -> String {
        if let Some(link) = &self.link
            && !link.is_empty()
        {
            return link.clone();
        }

        format!("https://youtube.com/watch?v={}", self.id)
    }

    pub fn get_chzzk_url(&self) -> &str {
        &self.chzzk_live_url
    }

    pub fn get_chzzk_live_url(&self) -> &str {
        self.get_chzzk_url()
    }

    pub fn get_twitch_url(&self) -> &str {
        &self.twitch_live_url
    }

    pub fn get_twitch_live_url(&self) -> &str {
        self.get_twitch_url()
    }

    pub fn has_youtube_info(&self) -> bool {
        !self.id.is_empty()
    }
}
