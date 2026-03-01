use std::collections::HashMap;

use shared_core::model::CommandType;

use super::{CommandParser, build_result};

pub struct HelpParser;

impl CommandParser for HelpParser {
    fn parse(
        &self,
        command: &str,
        _args: &[&str],
        _raw: &str,
    ) -> Option<shared_core::model::ParseResult> {
        if ["도움말", "도움", "help", "명령어", "commands"].contains(&command) {
            return Some(build_result(
                CommandType::Help,
                HashMap::new(),
                0.95,
                "help command",
            ));
        }

        None
    }
}
