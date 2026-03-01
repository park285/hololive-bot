use chrono::{DateTime, Utc};
use shared_core::model::stats::RankEntry;

use super::ResponseFormatter;

#[allow(clippy::too_many_arguments)]
pub trait StatsFormatting: Send + Sync {
    fn format_stats_top_gainers(&self, period_label: &str, gainers: &[RankEntry]) -> String;
    fn format_subscriber_count(&self, member_name: &str, subscribers: u64) -> String;
    fn format_subscriber_graph(
        &self,
        member_name: &str,
        days: i32,
        current: i64,
        change_7d: i64,
        change_30d: i64,
        sample_count: i32,
        updated_at: DateTime<Utc>,
        point_values: &[i64],
    ) -> String;
}

impl StatsFormatting for ResponseFormatter {
    fn format_stats_top_gainers(&self, period_label: &str, gainers: &[RankEntry]) -> String {
        if gainers.is_empty() {
            return self.decorate("통계 데이터가 없습니다.");
        }

        let mut lines = Vec::new();
        let period = period_label.trim();
        if period.is_empty() {
            lines.push("구독자 증가 순위".to_string());
        } else {
            lines.push(format!("구독자 증가 순위 ({period})"));
        }

        for entry in gainers {
            lines.push(format!(
                "{}. {} (+{}, 현재 {})",
                entry.rank, entry.member_name, entry.value, entry.current_subscribers
            ));
        }

        self.decorate(&lines.join("\n"))
    }

    fn format_subscriber_count(&self, member_name: &str, subscribers: u64) -> String {
        self.decorate(&format!("{member_name} 현재 구독자: {subscribers}"))
    }

    fn format_subscriber_graph(
        &self,
        member_name: &str,
        days: i32,
        current: i64,
        change_7d: i64,
        change_30d: i64,
        sample_count: i32,
        updated_at: DateTime<Utc>,
        point_values: &[i64],
    ) -> String {
        let graph = render_trend(point_values, 25);

        let mut lines = vec![
            format!("{member_name} 구독자 추이 ({days}일)"),
            format!("현재: {current}"),
            format!("7일 변화: {:+}", change_7d),
            format!("30일 변화: {:+}", change_30d),
        ];

        if !graph.is_empty() {
            lines.push(format!("추이: {graph}"));
        }

        lines.push(format!(
            "샘플: {sample_count}, 업데이트: {}",
            updated_at.format("%m-%d %H:%M UTC")
        ));

        self.decorate(&lines.join("\n"))
    }
}

#[allow(
    clippy::cast_precision_loss,
    clippy::cast_sign_loss,
    clippy::cast_possible_truncation
)]
fn render_trend(values: &[i64], width: usize) -> String {
    if values.is_empty() {
        return String::new();
    }

    let sampled = downsample(values, width);
    let min = sampled.iter().min().copied().unwrap_or(0);
    let max = sampled.iter().max().copied().unwrap_or(0);
    let range = (max - min).max(1) as f64;
    let levels = ['.', ':', '-', '=', '+', '*', '#', '%', '@'];

    sampled
        .iter()
        .map(|value| {
            let normalized = (*value - min) as f64 / range;
            let index = (normalized * (levels.len() - 1) as f64).round() as usize;
            levels[index.min(levels.len() - 1)]
        })
        .collect()
}

#[allow(
    clippy::cast_precision_loss,
    clippy::cast_sign_loss,
    clippy::cast_possible_truncation
)]
fn downsample(values: &[i64], width: usize) -> Vec<i64> {
    if width == 0 || values.len() <= width {
        return values.to_vec();
    }

    let step = values.len() as f64 / width as f64;
    (0..width)
        .map(|idx| {
            let source_index = (idx as f64 * step).floor() as usize;
            values[source_index.min(values.len() - 1)]
        })
        .collect()
}
