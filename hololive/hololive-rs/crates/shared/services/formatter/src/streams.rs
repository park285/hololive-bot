use shared_core::model::{Channel, Stream};

use super::ResponseFormatter;

pub trait StreamsFormatting: Send + Sync {
    fn format_live_streams(&self, streams: &[Stream]) -> String;
    fn upcoming_streams(&self, streams: &[Stream], hours: i32) -> String;
    fn channel_schedule(&self, channel: &Channel, streams: &[Stream], days: i32) -> String;
    fn format_member_not_live(&self, member_name: &str) -> String;
    fn format_live_overflow_count(&self, extra_count: usize) -> String;
    fn format_member_no_upcoming(&self, member_name: &str, hours: i32) -> String;
    fn format_upcoming_overflow_count(&self, extra_count: usize) -> String;
}

impl StreamsFormatting for ResponseFormatter {
    fn format_live_streams(&self, streams: &[Stream]) -> String {
        if streams.is_empty() {
            return self.decorate("현재 라이브 방송이 없습니다.");
        }

        let body = streams
            .iter()
            .map(|stream| format!("- {}: {}", stream.channel_name, stream.title))
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!("현재 라이브\n{body}"))
    }

    fn upcoming_streams(&self, streams: &[Stream], hours: i32) -> String {
        if streams.is_empty() {
            return self.decorate(&format!("앞으로 {hours}시간 내 예정된 방송이 없습니다."));
        }

        let body = streams
            .iter()
            .map(|stream| {
                let minutes_until = stream.minutes_until_start();
                format!(
                    "- {}: {} ({}분 후)",
                    stream.channel_name, stream.title, minutes_until
                )
            })
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!("예정 방송\n{body}"))
    }

    fn channel_schedule(&self, channel: &Channel, streams: &[Stream], days: i32) -> String {
        if streams.is_empty() {
            return self.decorate(&format!(
                "{}의 {}일 스케줄이 없습니다.",
                channel.display_name(),
                days
            ));
        }

        let body = streams
            .iter()
            .map(|stream| {
                let scheduled = stream.start_scheduled.map_or_else(
                    || "시간 미정".to_string(),
                    |value| value.format("%m-%d %H:%M UTC").to_string(),
                );
                format!("- {scheduled}: {}", stream.title)
            })
            .collect::<Vec<_>>()
            .join("\n");

        self.decorate(&format!(
            "{} 스케줄 ({days}일)\n{body}",
            channel.display_name()
        ))
    }

    fn format_member_not_live(&self, member_name: &str) -> String {
        self.decorate(&format!("{member_name}은 현재 라이브 중이 아닙니다."))
    }

    fn format_live_overflow_count(&self, extra_count: usize) -> String {
        self.decorate(&format!("외 {extra_count}개의 라이브 방송이 더 있습니다."))
    }

    fn format_member_no_upcoming(&self, member_name: &str, hours: i32) -> String {
        self.decorate(&format!(
            "{member_name}의 {hours}시간 내 예정 방송이 없습니다."
        ))
    }

    fn format_upcoming_overflow_count(&self, extra_count: usize) -> String {
        self.decorate(&format!("외 {extra_count}개의 예정 방송이 더 있습니다."))
    }
}
