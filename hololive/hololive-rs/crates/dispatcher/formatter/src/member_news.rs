use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::ResponseFormatter;

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct MemberNewsSummaryItem {
    pub title: String,
    pub category: String,
    pub source: Option<String>,
    pub published_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct MemberNewsDigest {
    pub headline: String,
    pub top_items: Vec<MemberNewsSummaryItem>,
    pub more_summary: String,
    pub total_count: usize,
}

pub trait MemberNewsFormatting: Send + Sync {
    fn format_member_news_digest(&self, digest: &MemberNewsDigest) -> String;
    fn format_member_news_no_members(&self) -> String;
    fn format_member_news_subscribed(&self) -> String;
    fn format_member_news_already_subscribed(&self) -> String;
    fn format_member_news_unsubscribed(&self) -> String;
    fn format_member_news_not_subscribed(&self) -> String;
    fn format_member_news_status(&self, is_subscribed: bool) -> String;
}

impl MemberNewsFormatting for ResponseFormatter {
    fn format_member_news_digest(&self, digest: &MemberNewsDigest) -> String {
        if digest.top_items.is_empty() {
            return self.decorate("이번 기간의 멤버 뉴스가 없습니다.");
        }

        let mut lines = vec![digest.headline.clone()];
        for item in &digest.top_items {
            lines.push(format!("- [{}] {}", item.category, item.title));
        }
        if !digest.more_summary.trim().is_empty() {
            lines.push(String::new());
            lines.push(digest.more_summary.trim().to_owned());
        }

        self.decorate(&lines.join("\n"))
    }

    fn format_member_news_no_members(&self) -> String {
        self.decorate("뉴스 다이제스트를 보낼 멤버 알람이 없습니다.")
    }

    fn format_member_news_subscribed(&self) -> String {
        self.decorate("멤버 뉴스 구독을 켰습니다.")
    }

    fn format_member_news_already_subscribed(&self) -> String {
        self.decorate("이미 멤버 뉴스를 구독 중입니다.")
    }

    fn format_member_news_unsubscribed(&self) -> String {
        self.decorate("멤버 뉴스 구독을 껐습니다.")
    }

    fn format_member_news_not_subscribed(&self) -> String {
        self.decorate("멤버 뉴스를 구독 중이 아닙니다.")
    }

    fn format_member_news_status(&self, is_subscribed: bool) -> String {
        if is_subscribed {
            self.decorate("멤버 뉴스 상태: ON")
        } else {
            self.decorate("멤버 뉴스 상태: OFF")
        }
    }
}
