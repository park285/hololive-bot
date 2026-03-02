use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::CommandType;

use super::{CommandParser, build_result, normalize_token};

pub struct StreamParser;

impl CommandParser for StreamParser {
    fn parse(
        &self,
        command: &str,
        args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if is_live_command(command) {
            let mut params = HashMap::new();
            let member = args.join(" ").trim().to_owned();
            if !member.is_empty() {
                params.insert("member".to_owned(), Value::String(member));
            }

            return Some(build_result(
                CommandType::Live,
                params,
                0.95,
                "stream live command",
            ));
        }

        if is_upcoming_command(command) {
            return Some(build_result(
                CommandType::Upcoming,
                parse_upcoming_args(args),
                0.95,
                "stream upcoming command",
            ));
        }

        if is_schedule_command(command) {
            return Some(build_result(
                CommandType::Schedule,
                parse_schedule_args(args),
                0.95,
                "stream schedule command",
            ));
        }

        None
    }
}

fn is_live_command(command: &str) -> bool {
    ["라이브", "live", "방송중", "생방송"].contains(&command)
}

fn is_upcoming_command(command: &str) -> bool {
    ["예정", "upcoming"].contains(&command)
}

fn is_schedule_command(command: &str) -> bool {
    ["일정", "스케줄", "schedule", "멤버", "member"].contains(&command)
}

fn parse_upcoming_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();
    let mut member_tokens = Vec::new();
    let mut limit_set = false;

    for arg in args {
        let token = arg.trim();
        if token.is_empty() {
            continue;
        }

        let normalized = normalize_token(token);
        if ["전체", "전부", "모두", "all"].contains(&normalized.as_str()) {
            params.insert("all".to_owned(), Value::Bool(true));
            params.remove("limit");
            continue;
        }

        if !limit_set
            && let Ok(limit) = token.parse::<u64>()
            && limit > 0
        {
            params.insert(
                "limit".to_owned(),
                Value::Number(serde_json::Number::from(limit)),
            );
            limit_set = true;
            continue;
        }

        member_tokens.push(token.to_owned());
    }

    let member = member_tokens.join(" ").trim().to_owned();
    if !member.is_empty() {
        params.insert("member".to_owned(), Value::String(member));
    }

    params
}

fn parse_schedule_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();

    if let Some(member) = args.first() {
        let member_name = member.trim();
        if !member_name.is_empty() {
            params.insert("member".to_owned(), Value::String(member_name.to_owned()));
        }
    }

    let days = args
        .get(1)
        .and_then(|value| value.parse::<u64>().ok())
        .map_or(7, |value| value.clamp(1, 30));

    params.insert(
        "days".to_owned(),
        Value::Number(serde_json::Number::from(days)),
    );
    params
}
