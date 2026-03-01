use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::CommandType;

use super::{CommandParser, build_result, normalize_token};

pub struct AlarmParser;

impl CommandParser for AlarmParser {
    fn parse(
        &self,
        command: &str,
        args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if !is_alarm_command(command, args) {
            return None;
        }

        let (sub_command, rest_args) = compact_alarm_tokens(command, args);
        let normalized_sub_command = normalize_token(&sub_command);

        if ["추가", "설정", "set", "add"].contains(&normalized_sub_command.as_str()) {
            let mut params = HashMap::new();
            let (member, alarm_type) = extract_member_and_type(&rest_args);
            params.insert("action".to_string(), Value::String("add".to_string()));
            params.insert("member".to_string(), Value::String(member));
            params.insert("type".to_string(), Value::String(alarm_type));
            return Some(build_result(
                CommandType::AlarmAdd,
                params,
                0.98,
                "alarm add command",
            ));
        }

        if ["제거", "삭제", "remove", "del", "delete"].contains(&normalized_sub_command.as_str())
        {
            let mut params = HashMap::new();
            let (member, alarm_type) = extract_member_and_type(&rest_args);
            params.insert("action".to_string(), Value::String("remove".to_string()));
            params.insert("member".to_string(), Value::String(member));
            params.insert("type".to_string(), Value::String(alarm_type));
            return Some(build_result(
                CommandType::AlarmRemove,
                params,
                0.98,
                "alarm remove command",
            ));
        }

        if ["목록", "list", "show"].contains(&normalized_sub_command.as_str()) {
            let mut params = HashMap::new();
            params.insert("action".to_string(), Value::String("list".to_string()));
            return Some(build_result(
                CommandType::AlarmList,
                params,
                0.95,
                "alarm list command",
            ));
        }

        if ["초기화", "clear", "reset"].contains(&normalized_sub_command.as_str()) {
            let mut params = HashMap::new();
            params.insert("action".to_string(), Value::String("clear".to_string()));
            return Some(build_result(
                CommandType::AlarmClear,
                params,
                0.95,
                "alarm clear command",
            ));
        }

        let mut params = HashMap::new();
        params.insert("action".to_string(), Value::String("invalid".to_string()));
        params.insert(
            "sub_command".to_string(),
            Value::String(normalized_sub_command),
        );

        Some(build_result(
            CommandType::AlarmInvalid,
            params,
            0.6,
            "alarm invalid command",
        ))
    }
}

fn is_alarm_command(command: &str, args: &[&str]) -> bool {
    if ["알람", "알림", "알림설정", "알람설정", "alarm"].contains(&command) {
        return true;
    }

    args.first()
        .map(|token| normalize_token(token))
        .is_some_and(|token| {
            [
                "추가",
                "set",
                "add",
                "설정",
                "제거",
                "remove",
                "del",
                "삭제",
                "목록",
                "list",
                "초기화",
                "clear",
            ]
            .contains(&token.as_str())
        })
}

fn compact_alarm_tokens(command: &str, args: &[&str]) -> (String, Vec<String>) {
    let normalized_command = normalize_token(command);
    let mapped = match normalized_command.as_str() {
        "알람설정" | "알림설정" | "알람추가" | "알림추가" => Some("추가"),
        "알람목록" | "알림목록" | "알람리스트" | "알림리스트" => Some("목록"),
        "알람제거" | "알림제거" | "알람삭제" | "알림삭제" | "알람해제" | "알림해제" => {
            Some("제거")
        }
        "알람초기화" | "알림초기화" | "알람리셋" | "알림리셋" => Some("초기화"),
        _ => None,
    };

    if let Some(mapped_sub_command) = mapped {
        let mut rest = Vec::new();
        rest.extend(args.iter().map(|arg| (*arg).to_string()));
        return (mapped_sub_command.to_string(), rest);
    }

    let sub_command = args.first().map_or("목록", |arg| *arg).to_string();
    let rest = args.iter().skip(1).map(|arg| (*arg).to_string()).collect();
    (sub_command, rest)
}

fn extract_member_and_type(args: &[String]) -> (String, String) {
    if args.is_empty() {
        return (String::new(), String::new());
    }

    let Some(last) = args.last() else {
        return (String::new(), String::new());
    };

    let normalized_last = normalize_token(last);
    let alarm_type = match normalized_last.as_str() {
        "방송" | "라이브" | "live" => Some("방송"),
        "커뮤니티" | "community" => Some("커뮤니티"),
        "쇼츠" | "shorts" => Some("쇼츠"),
        "전체" | "all" => Some("전체"),
        _ => None,
    };

    if let Some(alarm_type) = alarm_type
        && args.len() > 1
    {
        let member = args[..args.len() - 1].join(" ");
        return (member.trim().to_string(), alarm_type.to_string());
    }

    (args.join(" ").trim().to_string(), String::new())
}
