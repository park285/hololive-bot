use super::ResponseFormatter;

pub trait HelpFormatting: Send + Sync {
    fn format_help(&self) -> String;
}

impl HelpFormatting for ResponseFormatter {
    fn format_help(&self) -> String {
        self.decorate("명령어: 라이브, 예정, 일정, 알람, 통계, 멤버, 이벤트")
    }
}
