use shared_core::model::{TalentProfile, Translated};

use super::ResponseFormatter;

pub trait ProfileFormatting: Send + Sync {
    fn format_talent_profile(&self, raw: &TalentProfile, translated: Option<&Translated>)
    -> String;
}

impl ProfileFormatting for ResponseFormatter {
    fn format_talent_profile(
        &self,
        raw: &TalentProfile,
        translated: Option<&Translated>,
    ) -> String {
        let mut lines = Vec::new();

        if let Some(translated) = translated {
            lines.push(translated.display_name.clone());
            lines.push(translated.catchphrase.clone());
            lines.push(String::new());
            lines.push(translated.summary.clone());
            for highlight in &translated.highlights {
                lines.push(format!("- {highlight}"));
            }
        } else {
            lines.push(format!("{} / {}", raw.english_name, raw.japanese_name));
            lines.push(raw.catchphrase.clone());
            lines.push(String::new());
            lines.push(raw.description.clone());
            for row in &raw.data_entries {
                lines.push(format!("{}: {}", row.label, row.value));
            }
        }

        self.decorate(&lines.join("\n"))
    }
}
