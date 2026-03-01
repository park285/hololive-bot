use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum CommandType {
    #[serde(rename = "live")]
    Live,
    #[serde(rename = "upcoming")]
    Upcoming,
    #[serde(rename = "schedule")]
    Schedule,
    #[serde(rename = "help")]
    Help,
    #[serde(rename = "alarm_add")]
    AlarmAdd,
    #[serde(rename = "alarm_remove")]
    AlarmRemove,
    #[serde(rename = "alarm_list")]
    AlarmList,
    #[serde(rename = "alarm_clear")]
    AlarmClear,
    #[serde(rename = "alarm_invalid")]
    AlarmInvalid,
    #[serde(rename = "member_info")]
    MemberInfo,
    #[serde(rename = "stats")]
    Stats,
    #[serde(rename = "subscriber")]
    Subscriber,
    #[serde(rename = "member_news")]
    MemberNews,
    #[serde(rename = "news_subscription")]
    MemberNewsSubscription,
    #[serde(rename = "major_event")]
    MajorEvent,
    #[serde(rename = "unknown")]
    Unknown,
}

impl CommandType {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Live => "live",
            Self::Upcoming => "upcoming",
            Self::Schedule => "schedule",
            Self::Help => "help",
            Self::AlarmAdd => "alarm_add",
            Self::AlarmRemove => "alarm_remove",
            Self::AlarmList => "alarm_list",
            Self::AlarmClear => "alarm_clear",
            Self::AlarmInvalid => "alarm_invalid",
            Self::MemberInfo => "member_info",
            Self::Stats => "stats",
            Self::Subscriber => "subscriber",
            Self::MemberNews => "member_news",
            Self::MemberNewsSubscription => "news_subscription",
            Self::MajorEvent => "major_event",
            Self::Unknown => "unknown",
        }
    }

    pub fn is_valid(self) -> bool {
        true
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ParseResult {
    pub command: CommandType,
    pub params: HashMap<String, serde_json::Value>,
    pub confidence: f64,
    pub reasoning: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ParseResults {
    pub single: Option<ParseResult>,
    pub multiple: Vec<ParseResult>,
}

impl ParseResults {
    pub fn is_single(&self) -> bool {
        self.single.is_some()
    }

    pub fn is_multiple(&self) -> bool {
        !self.multiple.is_empty()
    }

    pub fn commands(&self) -> Vec<&ParseResult> {
        if let Some(single) = &self.single {
            return vec![single];
        }

        self.multiple.iter().collect()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandContext {
    pub room: String,
    pub room_name: String,
    pub user_id: String,
    pub user_name: String,
    pub is_group_chat: bool,
    pub message: String,
    pub timestamp: DateTime<Utc>,
}
