use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::CommandType;

use super::{CommandParser, build_result, normalize_token};

pub struct MemberParser;

impl CommandParser for MemberParser {
    fn parse(
        &self,
        command: &str,
        args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if ["멤버", "member", "프로필", "profile", "정보", "info"].contains(&command) {
            return Some(build_result(
                CommandType::MemberInfo,
                parse_member_info_args(args),
                0.95,
                "member info command",
            ));
        }

        if [
            "뉴스알림",
            "뉴스구독",
            "newsalert",
            "newsalerts",
            "newssubscription",
        ]
        .contains(&command)
        {
            return Some(build_result(
                CommandType::MemberNewsSubscription,
                parse_member_news_subscription_args(args),
                0.95,
                "member news subscription command",
            ));
        }

        if ["뉴스", "news"].contains(&command) {
            return Some(build_result(
                CommandType::MemberNews,
                parse_member_news_args(args),
                0.9,
                "member news command",
            ));
        }

        None
    }
}

fn parse_member_info_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();
    let query = args.join(" ").trim().to_string();
    if !query.is_empty() {
        params.insert("query".to_string(), Value::String(query));
    }
    params
}

fn parse_member_news_subscription_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();

    let action = args.first().map(|arg| normalize_token(arg)).map_or_else(
        || "status".to_string(),
        |arg| match arg.as_str() {
            "켜기" | "on" | "구독" => "on".to_string(),
            "끄기" | "off" | "해제" => "off".to_string(),
            _ => "status".to_string(),
        },
    );

    params.insert("action".to_string(), Value::String(action));
    params
}

fn parse_member_news_args(args: &[&str]) -> HashMap<String, Value> {
    let mut params = HashMap::new();

    let token = normalize_token(&args.join(""));
    let period = match token.as_str() {
        "이번달" | "월간" | "monthly" => "monthly",
        _ => "weekly",
    };

    params.insert("period".to_string(), Value::String(period.to_string()));
    params
}
