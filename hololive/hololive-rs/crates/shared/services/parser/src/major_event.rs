use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::CommandType;

use super::{CommandParser, build_result, normalize_token};

pub struct MajorEventParser;

impl CommandParser for MajorEventParser {
    fn parse(
        &self,
        command: &str,
        args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if !["이벤트", "행사", "행사알림", "이벤트알림"].contains(&command) {
            return None;
        }

        let action = args.first().map(|arg| normalize_token(arg)).map_or_else(
            || "status".to_owned(),
            |arg| match arg.as_str() {
                "켜기" | "on" | "구독" => "on".to_owned(),
                "끄기" | "off" | "해제" => "off".to_owned(),
                _ => "status".to_owned(),
            },
        );

        let mut params = HashMap::new();
        params.insert("action".to_owned(), Value::String(action));

        Some(build_result(
            CommandType::MajorEvent,
            params,
            0.95,
            "major event command",
        ))
    }
}
