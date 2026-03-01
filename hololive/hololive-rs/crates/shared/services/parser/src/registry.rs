use super::{
    CommandParser, alarm::AlarmParser, help::HelpParser, major_event::MajorEventParser,
    member::MemberParser, stats::StatsParser, stream::StreamParser,
};

pub fn default_command_parsers() -> Vec<Box<dyn CommandParser>> {
    vec![
        Box::new(StreamParser),
        Box::new(AlarmParser),
        Box::new(HelpParser),
        Box::new(StatsParser),
        Box::new(MemberParser),
        Box::new(MajorEventParser),
    ]
}
