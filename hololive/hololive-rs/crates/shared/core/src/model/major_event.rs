use chrono::{DateTime, NaiveDate, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub enum MajorEventStatus {
    #[default]
    Active,
    Ended,
    Canceled,
}

impl MajorEventStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Active => "active",
            Self::Ended => "ended",
            Self::Canceled => "canceled",
        }
    }
}

impl std::fmt::Display for MajorEventStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub enum MajorEventType {
    #[default]
    Event,
    News,
}

impl MajorEventType {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Event => "event",
            Self::News => "news",
        }
    }
}

impl std::fmt::Display for MajorEventType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub enum MajorEventLinkStatus {
    #[default]
    Unchecked,
    Ok,
    Failed,
    Blocked,
}

impl MajorEventLinkStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Unchecked => "unchecked",
            Self::Ok => "ok",
            Self::Failed => "failed",
            Self::Blocked => "blocked",
        }
    }
}

impl std::fmt::Display for MajorEventLinkStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MajorEvent {
    pub id: i32,
    pub external_id: String,
    pub event_type: MajorEventType,
    pub title: String,
    pub link: String,
    pub description: Option<String>,
    pub members: Vec<String>,
    pub pub_date: Option<DateTime<Utc>>,
    pub event_start_date: Option<NaiveDate>,
    pub event_end_date: Option<NaiveDate>,
    #[serde(skip)]
    pub event_dates: Vec<NaiveDate>,
    pub status: MajorEventStatus,
    pub link_status: MajorEventLinkStatus,
    pub link_checked_at: Option<DateTime<Utc>>,
    pub notified_at: Option<DateTime<Utc>>,
    pub notified_week: Option<String>,
    pub notified_month: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl MajorEvent {
    pub fn set_event_dates_from_parsed(&mut self) {
        if self.event_dates.is_empty() {
            return;
        }
        self.event_dates.sort_unstable();
        self.event_start_date = self.event_dates.first().copied();
        self.event_end_date = self.event_dates.last().copied();
    }

    pub fn apply_fallback_event_date(&mut self) {
        if self.event_start_date.is_some() {
            if self.event_end_date.is_none() {
                self.event_end_date = self.event_start_date;
            }
            return;
        }

        if let Some(pub_date) = self.pub_date {
            let date = pub_date.date_naive();
            self.event_start_date = Some(date);
            self.event_end_date = Some(date);
        }
    }
}
