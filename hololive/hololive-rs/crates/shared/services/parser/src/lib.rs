pub mod alarm;
pub mod help;
pub mod major_event;
pub mod member;
pub mod registry;
pub mod stats;
pub mod stream;

use std::collections::HashMap;

use serde_json::Value;
use shared_core::model::{CommandType, ParseResult, ParseResults};

pub trait CommandParser: Send + Sync {
    fn parse(&self, command: &str, args: &[&str], raw: &str) -> Option<ParseResult>;
}

pub struct MessageAdapter {
    prefix: String,
    parsers: Vec<Box<dyn CommandParser>>,
}

impl MessageAdapter {
    pub fn new(prefix: &str) -> Self {
        Self {
            prefix: normalize_prefix(prefix),
            parsers: registry::default_command_parsers(),
        }
    }

    pub fn with_parsers(prefix: &str, parsers: Vec<Box<dyn CommandParser>>) -> Self {
        Self {
            prefix: normalize_prefix(prefix),
            parsers,
        }
    }

    pub fn parse_message(&self, message: &str) -> ParseResult {
        let normalized_message = message.trim();
        let Some(command_text) = extract_command_text(normalized_message, &self.prefix) else {
            return unknown_result(normalized_message);
        };

        let parts: Vec<&str> = command_text.split_whitespace().collect();
        if parts.is_empty() {
            return unknown_result(normalized_message);
        }

        let command = normalize_token(parts[0]);
        let args = &parts[1..];

        for parser in &self.parsers {
            if let Some(result) = parser.parse(&command, args, normalized_message) {
                return result;
            }
        }

        unknown_result(normalized_message)
    }

    pub fn parse_results(&self, message: &str) -> ParseResults {
        ParseResults {
            single: Some(self.parse_message(message)),
            multiple: Vec::new(),
        }
    }
}

pub(crate) fn build_result(
    command: CommandType,
    params: HashMap<String, Value>,
    confidence: f64,
    reasoning: &str,
) -> ParseResult {
    ParseResult {
        command,
        params,
        confidence,
        reasoning: reasoning.to_owned(),
    }
}

pub(crate) fn normalize_token(token: &str) -> String {
    token.trim().to_lowercase()
}

fn unknown_result(raw: &str) -> ParseResult {
    build_result(
        CommandType::Unknown,
        HashMap::new(),
        0.0,
        &format!("no parser matched: {raw}"),
    )
}

fn normalize_prefix(prefix: &str) -> String {
    let trimmed = prefix.trim();
    if trimmed.is_empty() {
        "!".to_owned()
    } else {
        trimmed.to_owned()
    }
}

fn extract_command_text<'a>(message: &'a str, prefix: &str) -> Option<&'a str> {
    let legacy_prefixes = ["!", "/"];

    if let Some(rest) = message.strip_prefix(prefix) {
        let command = rest.trim();
        return (!command.is_empty()).then_some(command);
    }

    for legacy_prefix in legacy_prefixes {
        if prefix != legacy_prefix
            && let Some(rest) = message.strip_prefix(legacy_prefix)
        {
            let command = rest.trim();
            return (!command.is_empty()).then_some(command);
        }
    }

    None
}
