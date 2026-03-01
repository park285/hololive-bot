use shared_core::model::MajorEvent;

use super::ResponseFormatter;

pub trait MajorEventFormatting: Send + Sync {
    fn format_major_event_weekly_summary(&self, events: &[MajorEvent], llm_summary: &str)
    -> String;
    fn format_major_event_monthly_summary(
        &self,
        events: &[MajorEvent],
        llm_summary: &str,
    ) -> String;
    fn format_major_event_subscribed(&self) -> String;
    fn format_major_event_unsubscribed(&self) -> String;
    fn format_major_event_already_subscribed(&self) -> String;
    fn format_major_event_not_subscribed(&self) -> String;
    fn format_major_event_status(&self, is_subscribed: bool) -> String;
    fn format_major_event_usage(&self) -> String;
}

impl MajorEventFormatting for ResponseFormatter {
    fn format_major_event_weekly_summary(
        &self,
        events: &[MajorEvent],
        llm_summary: &str,
    ) -> String {
        format_event_summary(self, "주간", events, llm_summary)
    }

    fn format_major_event_monthly_summary(
        &self,
        events: &[MajorEvent],
        llm_summary: &str,
    ) -> String {
        format_event_summary(self, "월간", events, llm_summary)
    }

    fn format_major_event_subscribed(&self) -> String {
        self.decorate("이벤트 알림 구독을 켰습니다.")
    }

    fn format_major_event_unsubscribed(&self) -> String {
        self.decorate("이벤트 알림 구독을 껐습니다.")
    }

    fn format_major_event_already_subscribed(&self) -> String {
        self.decorate("이미 이벤트 알림을 구독 중입니다.")
    }

    fn format_major_event_not_subscribed(&self) -> String {
        self.decorate("이벤트 알림을 구독 중이 아닙니다.")
    }

    fn format_major_event_status(&self, is_subscribed: bool) -> String {
        if is_subscribed {
            self.decorate("이벤트 알림 상태: ON")
        } else {
            self.decorate("이벤트 알림 상태: OFF")
        }
    }

    fn format_major_event_usage(&self) -> String {
        self.decorate("사용법: !이벤트 [켜기|끄기|상태]")
    }
}

fn format_event_summary(
    formatter: &ResponseFormatter,
    label: &str,
    events: &[MajorEvent],
    llm_summary: &str,
) -> String {
    if events.is_empty() {
        return formatter.decorate(&format!("{label} 이벤트가 없습니다."));
    }

    let mut lines = Vec::new();
    lines.push(format!("{label} 이벤트 요약"));

    if !llm_summary.trim().is_empty() {
        lines.push(llm_summary.trim().to_string());
        lines.push(String::new());
    }

    for event in events.iter().take(10) {
        let date_text = event
            .event_start_date
            .map_or_else(|| "TBA".to_string(), |date| date.to_string());
        lines.push(format!("- [{}] {}", date_text, event.title));
    }

    formatter.decorate(&lines.join("\n"))
}
