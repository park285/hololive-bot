use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::{CommandType, stats::normalize_stats_period_token};

use super::{CommandParser, build_result, normalize_token};

pub struct StatsParser;

impl CommandParser for StatsParser {
    fn parse(
        &self,
        command: &str,
        args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if ["구독자", "subscriber", "subs"].contains(&command) {
            let mut params = HashMap::new();
            params.insert(
                "member".to_owned(),
                Value::String(args.join(" ").trim().to_owned()),
            );
            return Some(build_result(
                CommandType::Subscriber,
                params,
                0.95,
                "subscriber command",
            ));
        }

        if ["구독자순위", "순위", "통계", "stats", "ranking"].contains(&command) {
            return Some(build_result(
                CommandType::Stats,
                parse_stats_args(args),
                0.95,
                "stats command",
            ));
        }

        None
    }
}

fn parse_stats_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();
    params.insert("action".to_owned(), Value::String("gainers".to_owned()));

    for arg in args {
        let token = arg.trim();
        if token.is_empty() {
            continue;
        }

        if let Some((key, value)) = token.split_once('=') {
            let normalized_key = normalize_token(key);
            let normalized_period = normalize_stats_period_token(value);

            if is_period_key(&normalized_key) {
                let period = if normalized_period.is_empty() {
                    value.trim().to_owned()
                } else {
                    normalized_period
                };
                params.insert("period".to_owned(), Value::String(period));
                continue;
            }
        }

        let normalized_period = normalize_stats_period_token(token);
        if !normalized_period.is_empty() {
            params.insert("period".to_owned(), Value::String(normalized_period));
        }
    }

    params
}

fn is_period_key(key: &str) -> bool {
    ["period", "기간", "주기", "순위", "랭킹", "구독자", "통계"].contains(&key)
}
